package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "modernc.org/sqlite"
)

type fakeMarketProvider struct{}

func (fakeMarketProvider) Search(_ context.Context, query, market string) ([]marketSearchResult, error) {
	return []marketSearchResult{{Symbol: "QQQ", Name: "Invesco QQQ", Market: market, Currency: "USD"}}, nil
}
func (fakeMarketProvider) Quote(_ context.Context, symbol string) (marketQuote, error) {
	return marketQuote{Symbol: symbol, Price: 100, Currency: "USD"}, nil
}

func TestMigrate(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('assets','transactions')`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 tables, got %d", count)
	}
}

func TestMCPTools(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := (&app{db: db, market: fakeMarketProvider{}}).newMCPServer().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "capitrack-test", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "add_transaction", Arguments: map[string]any{
		"symbol": "2330", "name": "台積電", "type": "buy", "quantity": 10, "price": 900,
		"tradedAt": "2026-08-08", "note": "MCP test",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("add_transaction returned an error: %#v", result.Content)
	}

	result, err = clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "get_portfolio", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || result.StructuredContent == nil {
		t.Fatalf("get_portfolio returned invalid result: %#v", result)
	}
	for _, call := range []*mcp.CallToolParams{
		{Name: "get_portfolio_history", Arguments: map[string]any{"range": "1M"}},
		{Name: "get_performance_attribution", Arguments: map[string]any{"range": "1Y"}},
		{Name: "search_assets", Arguments: map[string]any{"query": "QQQ", "market": "US"}},
		{Name: "set_base_currency", Arguments: map[string]any{"currency": "USD"}},
	} {
		result, err = clientSession.CallTool(ctx, call)
		if err != nil {
			t.Fatalf("%s failed: %v", call.Name, err)
		}
		if result.IsError || result.StructuredContent == nil {
			detail := ""
			if len(result.Content) > 0 {
				if text, ok := result.Content[0].(*mcp.TextContent); ok {
					detail = text.Text
				}
			}
			t.Fatalf("%s returned invalid result: %s %#v", call.Name, detail, result)
		}
	}
}

func TestValidate(t *testing.T) {
	tx := transaction{Symbol: " 2330 ", Name: "台積電", Type: "BUY", Quantity: 10, Price: 900, TradedAt: "2026-08-08"}
	if err := validate(&tx); err != nil {
		t.Fatal(err)
	}
	if tx.Symbol != "2330" || tx.Type != "buy" {
		t.Fatalf("normalization failed: %#v", tx)
	}
}

func TestValidateUSWarrant(t *testing.T) {
	tx := transaction{Symbol: "AAPLW", Name: "Apple Warrant", AssetType: "warrant", Market: "US", Currency: "USD", UnderlyingSymbol: "AAPL", WarrantType: "call", ExpiryDate: "2027-12-17", StrikePrice: 200, ExerciseRatio: 1, Type: "buy", Quantity: 10, Price: 5, TradedAt: "2026-08-08"}
	if err := validate(&tx); err != nil {
		t.Fatal(err)
	}
	tx.Market = "TW"
	if err := validate(&tx); err == nil {
		t.Fatal("expected non-US warrant to be rejected")
	}
}

func TestTrailingStopRaisesTriggerWithNewHigh(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	a := &app{db: db}
	tx := transaction{Symbol: "QQQ", Name: "Invesco QQQ", Market: "US", Currency: "USD", Type: "buy", Quantity: 1, Price: 600, TradedAt: "2026-08-08"}
	if err := a.insertTransaction(&tx); err != nil {
		t.Fatal(err)
	}
	alert, err := a.addAlert(alertInput{Symbol: "QQQ", RuleType: "trailing_stop_pct", Value: 15, OneShot: true})
	if err != nil {
		t.Fatal(err)
	}
	if alert.TriggerPrice != 510 {
		t.Fatalf("expected initial trigger 510, got %v", alert.TriggerPrice)
	}
	_, _ = db.Exec(`UPDATE assets SET current_price=700 WHERE symbol='QQQ'`)
	if count, err := a.checkAlerts(context.Background(), false); err != nil || count != 0 {
		t.Fatalf("unexpected new-high check: count=%d err=%v", count, err)
	}
	var trigger float64
	_ = db.QueryRow(`SELECT trigger_price FROM price_alerts WHERE id=?`, alert.ID).Scan(&trigger)
	if trigger != 595 {
		t.Fatalf("expected raised trigger 595, got %v", trigger)
	}
	_, _ = db.Exec(`UPDATE assets SET current_price=595 WHERE symbol='QQQ'`)
	if count, err := a.checkAlerts(context.Background(), false); err != nil || count != 1 {
		t.Fatalf("expected one trigger: count=%d err=%v", count, err)
	}
}

func TestPortfolioConvertsCurrencies(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	a := &app{db: db}
	tw := transaction{Symbol: "2330.TW", Name: "台積電", Market: "TW", Currency: "TWD", Type: "buy", Quantity: 10, Price: 1000, TradedAt: "2026-08-08"}
	us := transaction{Symbol: "AAPL", Name: "Apple", Market: "US", Currency: "USD", Type: "buy", Quantity: 2, Price: 200, TradedAt: "2026-08-08"}
	if err := a.insertTransaction(&tw); err != nil {
		t.Fatal(err)
	}
	if err := a.insertTransaction(&us); err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`UPDATE assets SET current_price=1100 WHERE symbol='2330.TW'`)
	_, _ = db.Exec(`UPDATE assets SET current_price=210 WHERE symbol='AAPL'`)
	_, _ = db.Exec(`UPDATE settings SET value='32' WHERE key='usd_twd'`)
	result, err := a.calculatePortfolio()
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.TotalValue != 24440 {
		t.Fatalf("expected TWD 24440, got %v", result.Summary.TotalValue)
	}
	_, _ = db.Exec(`UPDATE settings SET value='USD' WHERE key='base_currency'`)
	result, err = a.calculatePortfolio()
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.TotalValue != 763.75 {
		t.Fatalf("expected USD 763.75, got %v", result.Summary.TotalValue)
	}
}

func TestLiabilitiesReduceNetWorth(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	a := &app{db: db}
	tx := transaction{Symbol: "AAPL", Name: "Apple", Market: "US", Currency: "USD", Type: "buy", Quantity: 10, Price: 100, TradedAt: "2026-08-08"}
	if err := a.insertTransaction(&tx); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO liabilities(name,category,currency,initial_balance,interest_rate,note) VALUES('房貸','mortgage','TWD',10000,2,'')`)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	_, err = db.Exec(`INSERT INTO liability_transactions(liability_id,type,amount,traded_at,note) VALUES(?,?,?,?,?)`, id, "repay", 1000, "2026-08-08", "")
	if err != nil {
		t.Fatal(err)
	}
	portfolio, err := a.calculatePortfolio()
	if err != nil {
		t.Fatal(err)
	}
	if portfolio.Summary.TotalValue != 30000 || portfolio.Summary.TotalLiabilities != 9000 || portfolio.Summary.NetWorth != 21000 {
		t.Fatalf("unexpected summary: %#v", portfolio.Summary)
	}
}

func TestPortfolioSnapshot(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	a := &app{db: db}
	tx := transaction{Symbol: "AAPL", Name: "Apple", Market: "US", Currency: "USD", Type: "buy", Quantity: 2, Price: 100, TradedAt: "2026-08-08"}
	if err := a.insertTransaction(&tx); err != nil {
		t.Fatal(err)
	}
	var total, rate float64
	if err := db.QueryRow(`SELECT total_twd,usd_twd FROM portfolio_snapshots ORDER BY id DESC LIMIT 1`).Scan(&total, &rate); err != nil {
		t.Fatal(err)
	}
	if total != 6000 || rate != 30 {
		t.Fatalf("unexpected snapshot total=%v rate=%v", total, rate)
	}
}

func TestPortfolioAttribution(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	a := &app{db: db}
	tx := transaction{Symbol: "QQQ", Name: "Invesco QQQ", Market: "US", Currency: "USD", Type: "buy", Quantity: 10, Price: 100, TradedAt: "2026-08-08"}
	if err := a.insertTransaction(&tx); err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`UPDATE assets SET current_price=120 WHERE symbol='QQQ'`)
	if err := a.recordSnapshot(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/portfolio/attribution?range=ALL", nil)
	rec := httptest.NewRecorder()
	a.portfolioAttribution(rec, req)
	if rec.Code != 200 {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		ProfitChange float64           `json:"profitChange"`
		Items        []attributionItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ProfitChange != 6000 {
		t.Fatalf("expected TWD 6000 profit contribution, got %v", body.ProfitChange)
	}
	if len(body.Items) != 1 || body.Items[0].Symbol != "QQQ" {
		t.Fatalf("unexpected attribution: %#v", body.Items)
	}
}
