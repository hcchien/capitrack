package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "modernc.org/sqlite"
)

type app struct {
	db     *sql.DB
	market marketDataProvider
}

type transaction struct {
	ID               int64   `json:"id"`
	Symbol           string  `json:"symbol"`
	Name             string  `json:"name"`
	Type             string  `json:"type"`
	PositionAction   string  `json:"positionAction,omitempty"`
	Quantity         float64 `json:"quantity"`
	Price            float64 `json:"price"`
	Fee              float64 `json:"fee"`
	TradedAt         string  `json:"tradedAt"`
	Note             string  `json:"note"`
	CreatedAt        string  `json:"createdAt,omitempty"`
	Market           string  `json:"market"`
	Currency         string  `json:"currency"`
	AssetType        string  `json:"assetType"`
	UnderlyingSymbol string  `json:"underlyingSymbol,omitempty"`
	WarrantType      string  `json:"warrantType,omitempty"`
	ExpiryDate       string  `json:"expiryDate,omitempty"`
	StrikePrice      float64 `json:"strikePrice,omitempty"`
	ExerciseRatio    float64 `json:"exerciseRatio,omitempty"`
}

type holding struct {
	Symbol           string  `json:"symbol"`
	Name             string  `json:"name"`
	Quantity         float64 `json:"quantity"`
	AverageCost      float64 `json:"averageCost"`
	CurrentPrice     float64 `json:"currentPrice"`
	MarketValue      float64 `json:"marketValue"`
	CostBasis        float64 `json:"costBasis"`
	Unrealized       float64 `json:"unrealized"`
	UnrealizedLocal  float64 `json:"unrealizedLocal"`
	UnrealizedPC     float64 `json:"unrealizedPercent"`
	Realized         float64 `json:"realized"`
	Allocation       float64 `json:"allocation"`
	Market           string  `json:"market"`
	Currency         string  `json:"currency"`
	AssetType        string  `json:"assetType"`
	UnderlyingSymbol string  `json:"underlyingSymbol,omitempty"`
	WarrantType      string  `json:"warrantType,omitempty"`
	ExpiryDate       string  `json:"expiryDate,omitempty"`
	StrikePrice      float64 `json:"strikePrice,omitempty"`
	ExerciseRatio    float64 `json:"exerciseRatio,omitempty"`
}

type portfolioSummary struct {
	TotalValue        float64 `json:"totalValue"`
	TotalCash         float64 `json:"totalCash"`
	TotalLiabilities  float64 `json:"totalLiabilities"`
	NetWorth          float64 `json:"netWorth"`
	TotalCost         float64 `json:"totalCost"`
	Unrealized        float64 `json:"unrealized"`
	UnrealizedPercent float64 `json:"unrealizedPercent"`
	Realized          float64 `json:"realized"`
}

type portfolioResult struct {
	Holdings     []holding        `json:"holdings"`
	Summary      portfolioSummary `json:"summary"`
	BaseCurrency string           `json:"baseCurrency"`
	USDTWD       float64          `json:"usdTwd"`
	LastUpdated  string           `json:"lastUpdated"`
}

func main() {
	dataDir := env("CAPITRACK_DATA_DIR", "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "capitrack.db")+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := migrate(db); err != nil {
		log.Fatal(err)
	}

	a := &app{db: db, market: newYahooProvider()}
	_ = a.recordSnapshot()
	mcpServer := a.newMCPServer()
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		if err := mcpServer.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Fatal(err)
		}
		return
	}
	go a.runAlertMonitor(context.Background(), 5*time.Minute)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/portfolio", a.portfolio)
	mux.HandleFunc("GET /api/portfolio/history", a.portfolioHistory)
	mux.HandleFunc("GET /api/portfolio/attribution", a.portfolioAttribution)
	mux.HandleFunc("GET /api/transactions", a.listTransactions)
	mux.HandleFunc("POST /api/transactions", a.createTransaction)
	mux.HandleFunc("PUT /api/transactions/{id}", a.updateTransaction)
	mux.HandleFunc("DELETE /api/transactions/{id}", a.deleteTransaction)
	mux.HandleFunc("PUT /api/assets/{symbol}/price", a.updatePrice)
	mux.HandleFunc("GET /api/markets/search", a.searchAssets)
	mux.HandleFunc("POST /api/markets/refresh", a.refreshMarketData)
	mux.HandleFunc("PUT /api/settings/base-currency", a.updateBaseCurrency)
	mux.HandleFunc("GET /api/cash-accounts", a.listCashAccounts)
	mux.HandleFunc("POST /api/cash-accounts", a.createCashAccount)
	mux.HandleFunc("DELETE /api/cash-accounts/{id}", a.deleteCashAccount)
	mux.HandleFunc("POST /api/cash-accounts/{id}/adjust", a.adjustCashAccount)
	mux.HandleFunc("PUT /api/cash-accounts/{id}/visibility", a.setCashAccountVisibility)
	mux.HandleFunc("GET /api/cash-transactions", a.listCashTransactions)
	mux.HandleFunc("POST /api/cash-transactions", a.createCashTransaction)
	mux.HandleFunc("DELETE /api/cash-transactions/{id}", a.deleteCashTransaction)
	mux.HandleFunc("GET /api/liabilities", a.listLiabilities)
	mux.HandleFunc("POST /api/liabilities", a.createLiability)
	mux.HandleFunc("PUT /api/liabilities/{id}", a.updateLiability)
	mux.HandleFunc("DELETE /api/liabilities/{id}", a.deleteLiability)
	mux.HandleFunc("GET /api/liability-transactions", a.listLiabilityTransactions)
	mux.HandleFunc("POST /api/liability-transactions", a.createLiabilityTransaction)
	mux.HandleFunc("DELETE /api/liability-transactions/{id}", a.deleteLiabilityTransaction)
	mux.HandleFunc("GET /api/alerts", a.listAlerts)
	mux.HandleFunc("POST /api/alerts", a.createAlert)
	mux.HandleFunc("PUT /api/alerts/{id}", a.updateAlert)
	mux.HandleFunc("DELETE /api/alerts/{id}", a.deleteAlert)
	mux.HandleFunc("POST /api/alerts/check", a.checkAlertsHTTP)
	mux.HandleFunc("GET /api/alert-events", a.listAlertEvents)
	mux.HandleFunc("POST /api/alert-events/{id}/read", a.readAlertEvent)
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, &mcp.StreamableHTTPOptions{JSONResponse: true, Stateless: true}))
	mux.Handle("/", http.FileServer(http.Dir("web")))
	port := env("PORT", "8080")
	log.Printf("CapiTrack is ready at http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, requestLogger(mux)))
}

func migrate(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS assets (
			symbol TEXT PRIMARY KEY COLLATE NOCASE,
			name TEXT NOT NULL,
			current_price REAL NOT NULL DEFAULT 0,
			market TEXT NOT NULL DEFAULT 'TW',
			currency TEXT NOT NULL DEFAULT 'TWD',
			asset_type TEXT NOT NULL DEFAULT 'stock',
			underlying_symbol TEXT NOT NULL DEFAULT '',
			warrant_type TEXT NOT NULL DEFAULT '',
			expiry_date TEXT NOT NULL DEFAULT '',
			strike_price REAL NOT NULL DEFAULT 0,
			exercise_ratio REAL NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS transactions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol TEXT NOT NULL COLLATE NOCASE REFERENCES assets(symbol) ON UPDATE CASCADE,
			type TEXT NOT NULL CHECK(type IN ('buy','sell')),
			quantity REAL NOT NULL CHECK(quantity > 0),
			price REAL NOT NULL CHECK(price >= 0),
			fee REAL NOT NULL DEFAULT 0 CHECK(fee >= 0),
			traded_at TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_transactions_symbol_date ON transactions(symbol, traded_at DESC)`,
		`CREATE TABLE IF NOT EXISTS cash_accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, currency TEXT NOT NULL CHECK(currency IN ('TWD','USD')),
			initial_balance REAL NOT NULL DEFAULT 0 CHECK(initial_balance >= 0), note TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS cash_transactions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, account_id INTEGER NOT NULL REFERENCES cash_accounts(id) ON DELETE CASCADE,
			type TEXT NOT NULL CHECK(type IN ('deposit','withdrawal','dividend','interest','fee','adjustment')),
			amount REAL NOT NULL CHECK(amount > 0), traded_at TEXT NOT NULL, note TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cash_transactions_account_date ON cash_transactions(account_id, traded_at DESC)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`INSERT OR IGNORE INTO settings(key,value) VALUES('base_currency','TWD')`,
		`INSERT OR IGNORE INTO settings(key,value) VALUES('usd_twd','30')`,
		`INSERT OR IGNORE INTO settings(key,value) VALUES('last_refresh','')`,
		`CREATE TABLE IF NOT EXISTS portfolio_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			captured_at TEXT NOT NULL,
			total_twd REAL NOT NULL,
			usd_twd REAL NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_portfolio_snapshots_time ON portfolio_snapshots(captured_at)`,
		`CREATE TABLE IF NOT EXISTS asset_performance_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			snapshot_id INTEGER NOT NULL REFERENCES portfolio_snapshots(id) ON DELETE CASCADE,
			symbol TEXT NOT NULL,
			name TEXT NOT NULL,
			pnl_twd REAL NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_asset_performance_snapshot ON asset_performance_snapshots(snapshot_id,symbol)`,
		`CREATE TABLE IF NOT EXISTS position_lots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol TEXT NOT NULL REFERENCES assets(symbol) ON UPDATE CASCADE,
			acquired_at TEXT NOT NULL,
			quantity REAL NOT NULL CHECK(quantity > 0),
			unit_cost REAL NOT NULL CHECK(unit_cost >= 0),
			source TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_position_lots_symbol_date ON position_lots(symbol, acquired_at)`,
		`CREATE TABLE IF NOT EXISTS price_alerts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol TEXT NOT NULL REFERENCES assets(symbol) ON UPDATE CASCADE,
			rule_type TEXT NOT NULL,
			value REAL NOT NULL,
			anchor_price REAL NOT NULL DEFAULT 0,
			high_watermark REAL NOT NULL DEFAULT 0,
			trigger_price REAL NOT NULL DEFAULT 0,
			active INTEGER NOT NULL DEFAULT 1,
			one_shot INTEGER NOT NULL DEFAULT 1,
			last_price REAL NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS alert_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			alert_id INTEGER NOT NULL REFERENCES price_alerts(id) ON DELETE CASCADE,
			symbol TEXT NOT NULL,
			rule_type TEXT NOT NULL,
			trigger_price REAL NOT NULL,
			market_price REAL NOT NULL,
			triggered_at TEXT NOT NULL,
			is_read INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS liabilities (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			category TEXT NOT NULL DEFAULT 'other',
			currency TEXT NOT NULL DEFAULT 'TWD',
			initial_balance REAL NOT NULL CHECK(initial_balance >= 0),
			interest_rate REAL NOT NULL DEFAULT 0 CHECK(interest_rate >= 0),
			note TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS liability_transactions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			liability_id INTEGER NOT NULL REFERENCES liabilities(id) ON DELETE CASCADE,
			type TEXT NOT NULL CHECK(type IN ('borrow','repay','interest','adjustment')),
			amount REAL NOT NULL CHECK(amount > 0),
			traded_at TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_liability_transactions_date ON liability_transactions(liability_id,traded_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	if err := ensureColumn(db, "assets", "market", `ALTER TABLE assets ADD COLUMN market TEXT NOT NULL DEFAULT 'TW'`); err != nil {
		return err
	}
	if err := ensureColumn(db, "assets", "currency", `ALTER TABLE assets ADD COLUMN currency TEXT NOT NULL DEFAULT 'TWD'`); err != nil {
		return err
	}
	for _, column := range []struct{ name, sql string }{{"asset_type", `ALTER TABLE assets ADD COLUMN asset_type TEXT NOT NULL DEFAULT 'stock'`}, {"underlying_symbol", `ALTER TABLE assets ADD COLUMN underlying_symbol TEXT NOT NULL DEFAULT ''`}, {"warrant_type", `ALTER TABLE assets ADD COLUMN warrant_type TEXT NOT NULL DEFAULT ''`}, {"expiry_date", `ALTER TABLE assets ADD COLUMN expiry_date TEXT NOT NULL DEFAULT ''`}, {"strike_price", `ALTER TABLE assets ADD COLUMN strike_price REAL NOT NULL DEFAULT 0`}, {"exercise_ratio", `ALTER TABLE assets ADD COLUMN exercise_ratio REAL NOT NULL DEFAULT 0`}} {
		if err := ensureColumn(db, "assets", column.name, column.sql); err != nil {
			return err
		}
	}
	if err := ensureColumn(db, "transactions", "position_action", `ALTER TABLE transactions ADD COLUMN position_action TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "cash_accounts", "hidden", `ALTER TABLE cash_accounts ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	return nil
}

func ensureColumn(db *sql.DB, table, column, statement string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid int
		var name, kind string
		var notnull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notnull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			rows.Close()
			return nil
		}
	}
	rows.Close()
	_, err = db.Exec(statement)
	return err
}

func (a *app) portfolio(w http.ResponseWriter, r *http.Request) {
	result, err := a.calculatePortfolio()
	if err != nil {
		fail(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (a *app) calculatePortfolio() (portfolioResult, error) {
	txs, err := a.queryTransactions("", "", "")
	if err != nil {
		return portfolioResult{}, err
	}
	type assetMeta struct {
		price                                      float64
		market, currency                           string
		assetType, underlying, warrantType, expiry string
		strikePrice                                float64
		exerciseRatio                              float64
	}
	assets := map[string]assetMeta{}
	rows, err := a.db.Query(`SELECT symbol, current_price, market, currency,asset_type,underlying_symbol,warrant_type,expiry_date,strike_price,exercise_ratio FROM assets`)
	if err != nil {
		return portfolioResult{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		var p float64
		var market, currency, assetType, underlying, warrantType, expiry string
		var strikePrice, ratio float64
		_ = rows.Scan(&s, &p, &market, &currency, &assetType, &underlying, &warrantType, &expiry, &strikePrice, &ratio)
		assets[s] = assetMeta{p, market, currency, assetType, underlying, warrantType, expiry, strikePrice, ratio}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return portfolioResult{}, err
	}
	rows.Close()
	type lotBasis struct{ quantity, cost float64 }
	lotBasisBySymbol := map[string]lotBasis{}
	lotRows, err := a.db.Query(`SELECT symbol,SUM(quantity),SUM(quantity*unit_cost) FROM position_lots GROUP BY symbol`)
	if err != nil {
		return portfolioResult{}, err
	}
	for lotRows.Next() {
		var symbol string
		var basis lotBasis
		if err := lotRows.Scan(&symbol, &basis.quantity, &basis.cost); err != nil {
			lotRows.Close()
			return portfolioResult{}, err
		}
		lotBasisBySymbol[symbol] = basis
	}
	if err := lotRows.Err(); err != nil {
		lotRows.Close()
		return portfolioResult{}, err
	}
	lotRows.Close()
	baseCurrency, usdTwd, lastUpdated, err := a.portfolioSettings()
	if err != nil {
		return portfolioResult{}, err
	}
	convert := func(value float64, from string) float64 {
		if from == baseCurrency {
			return value
		}
		if from == "USD" && baseCurrency == "TWD" {
			return value * usdTwd
		}
		if from == "TWD" && baseCurrency == "USD" && usdTwd > 0 {
			return value / usdTwd
		}
		return value
	}

	bySymbol := map[string]*holding{}
	for _, t := range txs {
		h := bySymbol[t.Symbol]
		if h == nil {
			meta := assets[t.Symbol]
			h = &holding{Symbol: t.Symbol, Name: t.Name, CurrentPrice: meta.price, Market: meta.market, Currency: meta.currency, AssetType: meta.assetType, UnderlyingSymbol: meta.underlying, WarrantType: meta.warrantType, ExpiryDate: meta.expiry, StrikePrice: meta.strikePrice, ExerciseRatio: meta.exerciseRatio}
			bySymbol[t.Symbol] = h
		}
		action := t.PositionAction
		multiplier := 1.0
		if h.AssetType == "warrant" && h.ExerciseRatio > 0 {
			multiplier = h.ExerciseRatio
		}
		if action == "" {
			action = t.Type + "_open"
		}
		if h.AssetType == "warrant" && action == "sell_open" {
			h.CostBasis -= t.Quantity*t.Price*multiplier - t.Fee
			h.Quantity -= t.Quantity
		} else if h.AssetType == "warrant" && action == "buy_close" {
			covered := min(t.Quantity, -h.Quantity)
			avg := 0.0
			if h.Quantity < 0 {
				avg = h.CostBasis / h.Quantity
			}
			h.Realized += covered*avg - covered*t.Price*multiplier - t.Fee
			h.CostBasis += covered * avg
			h.Quantity += covered
			if h.Quantity > -0.00000001 {
				h.Quantity, h.CostBasis = 0, 0
			}
		} else if action == "buy_open" {
			h.CostBasis += t.Quantity*t.Price*multiplier + t.Fee
			h.Quantity += t.Quantity
		} else {
			avg := 0.0
			if h.Quantity > 0 {
				avg = h.CostBasis / h.Quantity
			}
			sold := min(t.Quantity, h.Quantity)
			h.Realized += sold*t.Price*multiplier - t.Fee - sold*avg
			h.CostBasis -= sold * avg
			h.Quantity -= sold
			if h.Quantity < 0.00000001 {
				h.Quantity, h.CostBasis = 0, 0
			}
		}
	}
	holdings := make([]holding, 0)
	totalValue, totalCost, totalRealized := 0.0, 0.0, 0.0
	for _, h := range bySymbol {
		if h.Quantity != 0 {
			if basis, ok := lotBasisBySymbol[h.Symbol]; ok && math.Abs(basis.quantity-h.Quantity) < 0.000001 {
				h.CostBasis = basis.cost
			}
			h.AverageCost = h.CostBasis / h.Quantity
			multiplier := 1.0
			if h.AssetType == "warrant" && h.ExerciseRatio > 0 {
				multiplier = h.ExerciseRatio
			}
			localValue := h.Quantity * h.CurrentPrice * multiplier
			localCost := h.CostBasis
			localUnrealized := localValue - localCost
			if localCost != 0 {
				h.UnrealizedPC = localUnrealized / localCost * 100
			}
			h.MarketValue = convert(localValue, h.Currency)
			h.CostBasis = convert(localCost, h.Currency)
			h.Unrealized = convert(localUnrealized, h.Currency)
			h.UnrealizedLocal = localUnrealized
			h.Realized = convert(h.Realized, h.Currency)
			totalValue += h.MarketValue
			totalCost += h.CostBasis
			holdings = append(holdings, *h)
		}
		totalRealized += h.Realized
	}
	for i := range holdings {
		if totalValue > 0 {
			holdings[i].Allocation = holdings[i].MarketValue / totalValue * 100
		}
	}
	totalInvestmentValue := totalValue
	liabilities, err := a.calculateLiabilities(baseCurrency, usdTwd)
	if err != nil {
		return portfolioResult{}, err
	}
	totalLiabilities := 0.0
	for _, item := range liabilities {
		totalLiabilities += item.BalanceBase
	}
	cashAccounts, err := a.calculateCashAccounts(baseCurrency, usdTwd)
	if err != nil {
		return portfolioResult{}, err
	}
	totalCash := 0.0
	for _, item := range cashAccounts {
		totalCash += item.BalanceBase
	}
	totalValue += totalCash
	return portfolioResult{Holdings: holdings, Summary: portfolioSummary{
		TotalValue: totalValue, TotalCash: totalCash, TotalLiabilities: totalLiabilities, NetWorth: totalValue - totalLiabilities, TotalCost: totalCost, Unrealized: totalInvestmentValue - totalCost,
		UnrealizedPercent: percent(totalInvestmentValue-totalCost, totalCost), Realized: totalRealized,
	}, BaseCurrency: baseCurrency, USDTWD: usdTwd, LastUpdated: lastUpdated}, rows.Err()
}

func (a *app) portfolioSettings() (string, float64, string, error) {
	values := map[string]string{}
	rows, err := a.db.Query(`SELECT key,value FROM settings WHERE key IN ('base_currency','usd_twd','last_refresh')`)
	if err != nil {
		return "", 0, "", err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return "", 0, "", err
		}
		values[key] = value
	}
	rate, _ := strconv.ParseFloat(values["usd_twd"], 64)
	return values["base_currency"], rate, values["last_refresh"], rows.Err()
}

func (a *app) listTransactions(w http.ResponseWriter, r *http.Request) {
	txs, err := a.queryTransactions(r.URL.Query().Get("symbol"), r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		fail(w, err, 500)
		return
	}
	writeJSON(w, txs)
}

func (a *app) queryTransactions(symbol, from, to string) ([]transaction, error) {
	query := `SELECT t.id, t.symbol, a.name, t.type, t.position_action, t.quantity, t.price, t.fee, t.traded_at, t.note, t.created_at, a.market, a.currency,a.asset_type,a.underlying_symbol,a.warrant_type,a.expiry_date,a.strike_price,a.exercise_ratio
		FROM transactions t JOIN assets a ON a.symbol=t.symbol WHERE 1=1`
	args := []any{}
	if symbol != "" {
		query += ` AND t.symbol=?`
		args = append(args, strings.ToUpper(symbol))
	}
	if from != "" {
		query += ` AND t.traded_at>=?`
		args = append(args, from)
	}
	if to != "" {
		query += ` AND t.traded_at<=?`
		args = append(args, to)
	}
	query += ` ORDER BY t.traded_at ASC, t.id ASC`
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []transaction{}
	for rows.Next() {
		var t transaction
		if err := rows.Scan(&t.ID, &t.Symbol, &t.Name, &t.Type, &t.PositionAction, &t.Quantity, &t.Price, &t.Fee, &t.TradedAt, &t.Note, &t.CreatedAt, &t.Market, &t.Currency, &t.AssetType, &t.UnderlyingSymbol, &t.WarrantType, &t.ExpiryDate, &t.StrikePrice, &t.ExerciseRatio); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

func (a *app) createTransaction(w http.ResponseWriter, r *http.Request) {
	var t transaction
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		fail(w, errors.New("無法讀取交易內容"), 400)
		return
	}
	if err := validate(&t); err != nil {
		fail(w, err, 400)
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		fail(w, err, 500)
		return
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO assets(symbol,name,current_price,market,currency,asset_type,underlying_symbol,warrant_type,expiry_date,strike_price,exercise_ratio) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(symbol) DO UPDATE SET name=excluded.name,market=excluded.market,currency=excluded.currency,asset_type=excluded.asset_type,underlying_symbol=excluded.underlying_symbol,warrant_type=excluded.warrant_type,expiry_date=excluded.expiry_date,strike_price=excluded.strike_price,exercise_ratio=excluded.exercise_ratio`, t.Symbol, t.Name, t.Price, t.Market, t.Currency, t.AssetType, t.UnderlyingSymbol, t.WarrantType, t.ExpiryDate, t.StrikePrice, t.ExerciseRatio)
	if err != nil {
		fail(w, err, 500)
		return
	}
	result, err := tx.Exec(`INSERT INTO transactions(symbol,type,position_action,quantity,price,fee,traded_at,note) VALUES(?,?,?,?,?,?,?,?)`, t.Symbol, t.Type, t.PositionAction, t.Quantity, t.Price, t.Fee, t.TradedAt, t.Note)
	if err != nil {
		fail(w, err, 500)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err, 500)
		return
	}
	t.ID, _ = result.LastInsertId()
	_ = a.recordSnapshot()
	writeJSONStatus(w, t, 201)
}

func (a *app) updateTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, errors.New("無效的交易編號"), 400)
		return
	}
	var t transaction
	if err = json.NewDecoder(r.Body).Decode(&t); err != nil {
		fail(w, err, 400)
		return
	}
	if err = validate(&t); err != nil {
		fail(w, err, 400)
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		fail(w, err, 500)
		return
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO assets(symbol,name,current_price,market,currency,asset_type,underlying_symbol,warrant_type,expiry_date,strike_price,exercise_ratio) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(symbol) DO UPDATE SET name=excluded.name,market=excluded.market,currency=excluded.currency,asset_type=excluded.asset_type,underlying_symbol=excluded.underlying_symbol,warrant_type=excluded.warrant_type,expiry_date=excluded.expiry_date,strike_price=excluded.strike_price,exercise_ratio=excluded.exercise_ratio`, t.Symbol, t.Name, t.Price, t.Market, t.Currency, t.AssetType, t.UnderlyingSymbol, t.WarrantType, t.ExpiryDate, t.StrikePrice, t.ExerciseRatio)
	if err != nil {
		fail(w, err, 500)
		return
	}
	result, err := tx.Exec(`UPDATE transactions SET symbol=?,type=?,position_action=?,quantity=?,price=?,fee=?,traded_at=?,note=? WHERE id=?`, t.Symbol, t.Type, t.PositionAction, t.Quantity, t.Price, t.Fee, t.TradedAt, t.Note, id)
	if err != nil {
		fail(w, err, 500)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, errors.New("找不到交易"), 404)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err, 500)
		return
	}
	t.ID = id
	_ = a.recordSnapshot()
	writeJSON(w, t)
}

func (a *app) deleteTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, err, 400)
		return
	}
	result, err := a.db.Exec(`DELETE FROM transactions WHERE id=?`, id)
	if err != nil {
		fail(w, err, 500)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, errors.New("找不到交易"), 404)
		return
	}
	_ = a.recordSnapshot()
	w.WriteHeader(204)
}

func (a *app) updatePrice(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Price float64 `json:"price"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Price < 0 {
		fail(w, errors.New("請輸入有效價格"), 400)
		return
	}
	result, err := a.db.Exec(`UPDATE assets SET current_price=?,updated_at=CURRENT_TIMESTAMP WHERE symbol=?`, body.Price, strings.ToUpper(r.PathValue("symbol")))
	if err != nil {
		fail(w, err, 500)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, errors.New("找不到投資項目"), 404)
		return
	}
	_ = a.recordSnapshot()
	writeJSON(w, map[string]any{"ok": true})
}

type portfolioHistoryPoint struct {
	CapturedAt string  `json:"capturedAt"`
	Value      float64 `json:"value"`
}
type portfolioHistoryResult struct {
	Currency string                  `json:"currency"`
	Points   []portfolioHistoryPoint `json:"points"`
}

func (a *app) recordSnapshot() error {
	result, err := a.calculatePortfolio()
	if err != nil {
		return err
	}
	totalTWD := result.Summary.NetWorth
	if result.BaseCurrency == "USD" {
		totalTWD *= result.USDTWD
	}
	performance, err := a.assetPerformanceTWD(result.USDTWD)
	if err != nil {
		return err
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	insert, err := tx.Exec(`INSERT INTO portfolio_snapshots(captured_at,total_twd,usd_twd) VALUES(?,?,?)`, time.Now().Format(time.RFC3339), totalTWD, result.USDTWD)
	if err != nil {
		return err
	}
	snapshotID, _ := insert.LastInsertId()
	for _, item := range performance {
		if _, err = tx.Exec(`INSERT INTO asset_performance_snapshots(snapshot_id,symbol,name,pnl_twd) VALUES(?,?,?,?)`, snapshotID, item.Symbol, item.Name, item.PnLTWD); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *app) portfolioHistory(w http.ResponseWriter, r *http.Request) {
	result, err := a.getPortfolioHistory(r.URL.Query().Get("range"))
	if err != nil {
		fail(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (a *app) getPortfolioHistory(requestedRange string) (portfolioHistoryResult, error) {
	rangeName := strings.ToUpper(requestedRange)
	days := map[string]int{"7D": 7, "1M": 30, "3M": 90, "1Y": 365}
	query := `SELECT s.captured_at,s.total_twd,s.usd_twd FROM portfolio_snapshots s JOIN (SELECT substr(captured_at,1,10) AS snapshot_day,MAX(captured_at) AS latest_captured_at FROM portfolio_snapshots`
	args := []any{}
	if dayCount, ok := days[rangeName]; ok {
		query += ` WHERE captured_at>=?`
		args = append(args, time.Now().AddDate(0, 0, -dayCount).Format(time.RFC3339))
	} else if rangeName == "YTD" {
		query += ` WHERE captured_at>=?`
		args = append(args, time.Date(time.Now().Year(), time.January, 1, 0, 0, 0, 0, time.Local).Format(time.RFC3339))
	}
	query += ` GROUP BY snapshot_day) daily ON s.captured_at=daily.latest_captured_at ORDER BY s.captured_at ASC`
	baseCurrency, _, _, err := a.portfolioSettings()
	if err != nil {
		return portfolioHistoryResult{}, err
	}
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return portfolioHistoryResult{}, err
	}
	defer rows.Close()
	points := []portfolioHistoryPoint{}
	for rows.Next() {
		var point portfolioHistoryPoint
		var totalTWD, rate float64
		if err := rows.Scan(&point.CapturedAt, &totalTWD, &rate); err != nil {
			return portfolioHistoryResult{}, err
		}
		point.Value = totalTWD
		if baseCurrency == "USD" && rate > 0 {
			point.Value = totalTWD / rate
		}
		points = append(points, point)
	}
	return portfolioHistoryResult{Currency: baseCurrency, Points: points}, rows.Err()
}

func validate(t *transaction) error {
	t.Symbol = strings.ToUpper(strings.TrimSpace(t.Symbol))
	t.Name = strings.TrimSpace(t.Name)
	t.Type = strings.ToLower(t.Type)
	t.Note = strings.TrimSpace(t.Note)
	t.Market = strings.ToUpper(strings.TrimSpace(t.Market))
	t.Currency = strings.ToUpper(strings.TrimSpace(t.Currency))
	t.AssetType = strings.ToLower(strings.TrimSpace(t.AssetType))
	t.PositionAction = strings.ToLower(strings.TrimSpace(t.PositionAction))
	if t.AssetType == "" {
		t.AssetType = "stock"
	}
	t.UnderlyingSymbol = strings.ToUpper(strings.TrimSpace(t.UnderlyingSymbol))
	t.WarrantType = strings.ToLower(strings.TrimSpace(t.WarrantType))
	t.ExpiryDate = strings.TrimSpace(t.ExpiryDate)
	if t.Market == "" {
		t.Market = "TW"
	}
	if t.Currency == "" {
		if t.Market == "US" {
			t.Currency = "USD"
		} else {
			t.Currency = "TWD"
		}
	}
	if t.Symbol == "" || t.Name == "" {
		return errors.New("請填寫代號與名稱")
	}
	if t.Type != "buy" && t.Type != "sell" {
		return errors.New("交易類型必須是買入或賣出")
	}
	if t.Market != "TW" && t.Market != "US" {
		return errors.New("市場必須是台股或美股")
	}
	if t.Currency != "TWD" && t.Currency != "USD" {
		return errors.New("幣別必須是台幣或美金")
	}
	if t.AssetType != "stock" && t.AssetType != "etf" && t.AssetType != "warrant" {
		return errors.New("投資類型必須是股票、ETF 或權證")
	}
	if t.AssetType == "warrant" {
		if t.PositionAction == "" {
			t.PositionAction = t.Type + "_open"
		}
		validActions := map[string]bool{"buy_open": true, "sell_open": true, "buy_close": true, "sell_close": true}
		if !validActions[t.PositionAction] {
			return errors.New("權證部位動作不正確")
		}
		if strings.HasPrefix(t.PositionAction, "buy") {
			t.Type = "buy"
		} else {
			t.Type = "sell"
		}
		if t.Market != "US" || t.Currency != "USD" {
			return errors.New("目前權證交易僅支援美股與美金")
		}
		if t.WarrantType != "call" && t.WarrantType != "put" {
			return errors.New("權證必須是認購或認售")
		}
		if _, err := time.Parse("2006-01-02", t.ExpiryDate); err != nil {
			return errors.New("權證到期日不正確")
		}
		if t.UnderlyingSymbol == "" || t.StrikePrice <= 0 || t.ExerciseRatio <= 0 {
			return errors.New("請填寫權證連結標的、履約價與行使比例")
		}
	}
	if t.Quantity <= 0 || t.Price < 0 || t.Fee < 0 {
		return errors.New("數量、價格或手續費不正確")
	}
	if _, err := time.Parse("2006-01-02", t.TradedAt); err != nil {
		return errors.New("交易日期不正確")
	}
	return nil
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
func writeJSON(w http.ResponseWriter, v any) { writeJSONStatus(w, v, 200) }
func writeJSONStatus(w http.ResponseWriter, v any, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error, status int) {
	writeJSONStatus(w, map[string]string{"error": err.Error()}, status)
}
func percent(v, b float64) float64 {
	if b == 0 {
		return 0
	}
	return v / b * 100
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
