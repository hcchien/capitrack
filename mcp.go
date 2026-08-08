package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listTransactionsInput struct {
	Symbol string `json:"symbol,omitempty" jsonschema:"投資標的代號；留空表示全部"`
	From   string `json:"from,omitempty" jsonschema:"開始日期，格式 YYYY-MM-DD"`
	To     string `json:"to,omitempty" jsonschema:"結束日期，格式 YYYY-MM-DD"`
}

type transactionInput struct {
	Symbol           string  `json:"symbol" jsonschema:"投資標的代號，例如 2330 或 AAPL"`
	Name             string  `json:"name" jsonschema:"投資標的名稱"`
	Type             string  `json:"type" jsonschema:"交易類型，只能是 buy 或 sell"`
	Quantity         float64 `json:"quantity" jsonschema:"交易數量，必須大於 0"`
	Price            float64 `json:"price" jsonschema:"每單位成交價格"`
	Fee              float64 `json:"fee,omitempty" jsonschema:"交易手續費"`
	TradedAt         string  `json:"tradedAt" jsonschema:"交易日期，格式 YYYY-MM-DD"`
	Note             string  `json:"note,omitempty" jsonschema:"交易理由或備註"`
	Market           string  `json:"market,omitempty" jsonschema:"市場，只能是 TW 或 US；預設 TW"`
	Currency         string  `json:"currency,omitempty" jsonschema:"交易幣別，只能是 TWD 或 USD；依市場預設"`
	AssetType        string  `json:"assetType,omitempty" jsonschema:"投資類型：stock 或 warrant"`
	UnderlyingSymbol string  `json:"underlyingSymbol,omitempty" jsonschema:"權證連結標的代號"`
	WarrantType      string  `json:"warrantType,omitempty" jsonschema:"權證類型：call 或 put"`
	ExpiryDate       string  `json:"expiryDate,omitempty" jsonschema:"權證到期日 YYYY-MM-DD"`
	StrikePrice      float64 `json:"strikePrice,omitempty" jsonschema:"權證履約價"`
	ExerciseRatio    float64 `json:"exerciseRatio,omitempty" jsonschema:"權證行使比例"`
}

type priceInput struct {
	Symbol string  `json:"symbol" jsonschema:"要更新的投資標的代號"`
	Price  float64 `json:"price" jsonschema:"目前每單位市價"`
}

type rangeInput struct {
	Range string `json:"range,omitempty" jsonschema:"時間範圍；走勢可用 7D、1M、3M、ALL，績效歸因可用 1M、3M、1Y、ALL"`
}

type searchAssetsInput struct {
	Query  string `json:"query" jsonschema:"股票代號或公司名稱關鍵字"`
	Market string `json:"market" jsonschema:"市場，只能是 TW 或 US"`
}

type searchAssetsOutput struct {
	Results []marketSearchResult `json:"results"`
}
type baseCurrencyInput struct {
	Currency string `json:"currency" jsonschema:"總值顯示幣別，只能是 TWD 或 USD"`
}

type transactionListOutput struct {
	Transactions []transaction `json:"transactions"`
}

type mutationOutput struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Trade   transaction `json:"transaction,omitempty"`
}

type alertIDInput struct {
	ID int64 `json:"id" jsonschema:"提醒規則或事件 ID"`
}
type alertActiveInput struct {
	ID     int64 `json:"id"`
	Active bool  `json:"active"`
}
type alertListOutput struct {
	Alerts []priceAlert `json:"alerts"`
}
type alertEventsOutput struct {
	Events []alertEvent `json:"events"`
}
type alertCheckOutput struct {
	Triggered int `json:"triggered"`
}
type liabilityInput struct {
	Name           string  `json:"name"`
	Category       string  `json:"category,omitempty"`
	Currency       string  `json:"currency,omitempty"`
	InitialBalance float64 `json:"initialBalance"`
	InterestRate   float64 `json:"interestRate,omitempty"`
	Note           string  `json:"note,omitempty"`
}
type liabilityTransactionInput struct {
	LiabilityID int64   `json:"liabilityId"`
	Type        string  `json:"type"`
	Amount      float64 `json:"amount"`
	TradedAt    string  `json:"tradedAt"`
	Note        string  `json:"note,omitempty"`
}
type liabilitiesOutput struct {
	Liabilities []liability `json:"liabilities"`
}
type liabilityTransactionsOutput struct {
	Transactions []liabilityTransaction `json:"transactions"`
}

func (a *app) newMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "capitrack", Version: "0.2.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_portfolio", Description: "取得目前投資組合，包括持股、總市值、成本、配置比例與已實現／未實現損益。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, portfolioResult, error) {
		result, err := a.calculatePortfolio()
		return nil, result, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_portfolio_history", Description: "取得淨資產變化走勢的時間序列，可指定 7D、1M、3M 或 ALL。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input rangeInput) (*mcp.CallToolResult, portfolioHistoryResult, error) {
		result, err := a.getPortfolioHistory(input.Range)
		return nil, result, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_performance_attribution", Description: "取得指定期間的績效歸因，包括淨資產變化、投資損益、資金／負債變動，以及各標的損益貢獻。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input rangeInput) (*mcp.CallToolResult, attributionResult, error) {
		result, err := a.calculateAttribution(input.Range)
		return nil, result, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_transactions", Description: "查詢投資交易紀錄，可依標的與日期區間篩選。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input listTransactionsInput) (*mcp.CallToolResult, transactionListOutput, error) {
		items, err := a.queryTransactions(input.Symbol, input.From, input.To)
		return nil, transactionListOutput{Transactions: items}, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "search_assets", Description: "依股票代號或公司名稱搜尋台股、美股，回傳可用 ticker、名稱、幣別與市場。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input searchAssetsInput) (*mcp.CallToolResult, searchAssetsOutput, error) {
		market := strings.ToUpper(strings.TrimSpace(input.Market))
		if market != "TW" && market != "US" {
			return nil, searchAssetsOutput{}, errors.New("市場必須是 TW 或 US")
		}
		results, err := a.market.Search(ctx, strings.TrimSpace(input.Query), market)
		return nil, searchAssetsOutput{Results: results}, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "add_transaction", Description: "新增一筆買入或賣出交易；持股、成本與損益會自動重新計算。",
		Annotations: &mcp.ToolAnnotations{},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input transactionInput) (*mcp.CallToolResult, mutationOutput, error) {
		t := transaction{Symbol: input.Symbol, Name: input.Name, Type: input.Type, Quantity: input.Quantity, Price: input.Price, Fee: input.Fee, TradedAt: input.TradedAt, Note: input.Note, Market: input.Market, Currency: input.Currency, AssetType: input.AssetType, UnderlyingSymbol: input.UnderlyingSymbol, WarrantType: input.WarrantType, ExpiryDate: input.ExpiryDate, StrikePrice: input.StrikePrice, ExerciseRatio: input.ExerciseRatio}
		if err := a.insertTransaction(&t); err != nil {
			return nil, mutationOutput{}, err
		}
		return nil, mutationOutput{Success: true, Message: "交易已新增", Trade: t}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "set_base_currency", Description: "設定 portfolio 總值、走勢與績效歸因使用 TWD 或 USD 顯示。",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input baseCurrencyInput) (*mcp.CallToolResult, mutationOutput, error) {
		currency := strings.ToUpper(strings.TrimSpace(input.Currency))
		if currency != "TWD" && currency != "USD" {
			return nil, mutationOutput{}, errors.New("顯示幣別必須是 TWD 或 USD")
		}
		_, err := a.db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('base_currency',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, currency)
		if err != nil {
			return nil, mutationOutput{}, err
		}
		return nil, mutationOutput{Success: true, Message: "總值顯示幣別已更新"}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "update_current_price", Description: "更新投資標的目前市價，投資組合總值與未實現損益會同步更新。",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input priceInput) (*mcp.CallToolResult, mutationOutput, error) {
		if input.Price < 0 || strings.TrimSpace(input.Symbol) == "" {
			return nil, mutationOutput{}, errors.New("請提供有效的標的代號與價格")
		}
		result, err := a.db.ExecContext(ctx, `UPDATE assets SET current_price=?,updated_at=CURRENT_TIMESTAMP WHERE symbol=?`, input.Price, strings.ToUpper(strings.TrimSpace(input.Symbol)))
		if err != nil {
			return nil, mutationOutput{}, err
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			return nil, mutationOutput{}, sql.ErrNoRows
		}
		_ = a.recordSnapshot()
		return nil, mutationOutput{Success: true, Message: "目前價格已更新"}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "refresh_market_data", Description: "取得 portfolio 內所有台股、美股的最新價格，以及 USD/TWD 最新匯率。",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, mutationOutput, error) {
		rows, err := a.db.QueryContext(ctx, `SELECT symbol,market FROM assets WHERE EXISTS(SELECT 1 FROM transactions t WHERE t.symbol=assets.symbol)`)
		if err != nil {
			return nil, mutationOutput{}, err
		}
		defer rows.Close()
		updated := 0
		for rows.Next() {
			var symbol, market string
			if err := rows.Scan(&symbol, &market); err != nil {
				return nil, mutationOutput{}, err
			}
			lookup := symbol
			if market == "TW" && !strings.Contains(lookup, ".") {
				lookup += ".TW"
			}
			quote, err := a.market.Quote(ctx, lookup)
			if err != nil {
				continue
			}
			if _, err = a.db.ExecContext(ctx, `UPDATE assets SET current_price=?,currency=?,updated_at=CURRENT_TIMESTAMP WHERE symbol=?`, quote.Price, quote.Currency, symbol); err == nil {
				updated++
			}
		}
		if fx, quoteErr := a.market.Quote(ctx, "TWD=X"); quoteErr == nil {
			_, _ = a.db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('usd_twd',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, fmt.Sprintf("%.6f", fx.Price))
		}
		_, _ = a.db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('last_refresh',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, time.Now().Format(time.RFC3339))
		_ = a.recordSnapshot()
		triggered, _ := a.checkAlerts(ctx, false)
		return nil, mutationOutput{Success: true, Message: fmt.Sprintf("已更新 %d 個投資標的，觸發 %d 個提醒", updated, triggered)}, rows.Err()
	})
	mcp.AddTool(server, &mcp.Tool{Name: "list_price_alerts", Description: "列出價格、成本比例與移動停損提醒。", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, alertListOutput, error) {
		items, err := a.getAlerts()
		return nil, alertListOutput{Alerts: items}, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "create_price_alert", Description: "建立價格提醒；ruleType 可用 price_below、price_above、cost_loss_pct、cost_profit_pct、trailing_stop_pct。"}, func(ctx context.Context, req *mcp.CallToolRequest, input alertInput) (*mcp.CallToolResult, priceAlert, error) {
		item, err := a.addAlert(input)
		return nil, item, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "set_price_alert_active", Description: "啟用或暫停一個價格提醒。", Annotations: &mcp.ToolAnnotations{IdempotentHint: true}}, func(ctx context.Context, req *mcp.CallToolRequest, input alertActiveInput) (*mcp.CallToolResult, mutationOutput, error) {
		result, err := a.db.ExecContext(ctx, `UPDATE price_alerts SET active=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, input.Active, input.ID)
		if err != nil {
			return nil, mutationOutput{}, err
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			return nil, mutationOutput{}, sql.ErrNoRows
		}
		return nil, mutationOutput{Success: true, Message: "提醒狀態已更新"}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "delete_price_alert", Description: "刪除一個價格提醒。"}, func(ctx context.Context, req *mcp.CallToolRequest, input alertIDInput) (*mcp.CallToolResult, mutationOutput, error) {
		result, err := a.db.ExecContext(ctx, `DELETE FROM price_alerts WHERE id=?`, input.ID)
		if err != nil {
			return nil, mutationOutput{}, err
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			return nil, mutationOutput{}, sql.ErrNoRows
		}
		return nil, mutationOutput{Success: true, Message: "提醒已刪除"}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "list_alert_events", Description: "列出最近 100 筆價格提醒觸發紀錄。", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, alertEventsOutput, error) {
		items, err := a.getAlertEvents()
		return nil, alertEventsOutput{Events: items}, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "check_price_alerts", Description: "取得最新行情並立即檢查所有啟用中的價格提醒。", Annotations: &mcp.ToolAnnotations{IdempotentHint: true}}, func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, alertCheckOutput, error) {
		count, err := a.checkAlerts(ctx, true)
		return nil, alertCheckOutput{Triggered: count}, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "list_liabilities", Description: "列出所有負債、目前餘額與換算後金額。", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, liabilitiesOutput, error) {
		base, rate, _, err := a.portfolioSettings()
		if err != nil {
			return nil, liabilitiesOutput{}, err
		}
		items, err := a.calculateLiabilities(base, rate)
		return nil, liabilitiesOutput{Liabilities: items}, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "create_liability", Description: "新增負債項目，例如房貸、貸款或信用卡。"}, func(ctx context.Context, req *mcp.CallToolRequest, input liabilityInput) (*mcp.CallToolResult, liability, error) {
		item := liability{Name: input.Name, Category: input.Category, Currency: input.Currency, InitialBalance: input.InitialBalance, InterestRate: input.InterestRate, Note: input.Note}
		if err := validateLiability(&item); err != nil {
			return nil, liability{}, err
		}
		result, err := a.db.ExecContext(ctx, `INSERT INTO liabilities(name,category,currency,initial_balance,interest_rate,note) VALUES(?,?,?,?,?,?)`, item.Name, item.Category, item.Currency, item.InitialBalance, item.InterestRate, item.Note)
		if err != nil {
			return nil, liability{}, err
		}
		item.ID, _ = result.LastInsertId()
		_ = a.recordSnapshot()
		return nil, item, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "list_liability_transactions", Description: "列出借款、還款、利息與負債調整紀錄。", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, liabilityTransactionsOutput, error) {
		items, err := a.queryLiabilityTransactions()
		return nil, liabilityTransactionsOutput{Transactions: items}, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "add_liability_transaction", Description: "新增負債異動；type 可用 borrow、repay、interest 或 adjustment。"}, func(ctx context.Context, req *mcp.CallToolRequest, input liabilityTransactionInput) (*mcp.CallToolResult, liabilityTransaction, error) {
		item := liabilityTransaction{LiabilityID: input.LiabilityID, Type: input.Type, Amount: input.Amount, TradedAt: input.TradedAt, Note: input.Note}
		if err := validateLiabilityTransaction(&item); err != nil {
			return nil, liabilityTransaction{}, err
		}
		result, err := a.db.ExecContext(ctx, `INSERT INTO liability_transactions(liability_id,type,amount,traded_at,note) VALUES(?,?,?,?,?)`, item.LiabilityID, item.Type, item.Amount, item.TradedAt, item.Note)
		if err != nil {
			return nil, liabilityTransaction{}, err
		}
		item.ID, _ = result.LastInsertId()
		_ = a.recordSnapshot()
		return nil, item, nil
	})
	return server
}

func (a *app) insertTransaction(t *transaction) error {
	if err := validate(t); err != nil {
		return err
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO assets(symbol,name,current_price,market,currency,asset_type,underlying_symbol,warrant_type,expiry_date,strike_price,exercise_ratio) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(symbol) DO UPDATE SET name=excluded.name,market=excluded.market,currency=excluded.currency,asset_type=excluded.asset_type,underlying_symbol=excluded.underlying_symbol,warrant_type=excluded.warrant_type,expiry_date=excluded.expiry_date,strike_price=excluded.strike_price,exercise_ratio=excluded.exercise_ratio`, t.Symbol, t.Name, t.Price, t.Market, t.Currency, t.AssetType, t.UnderlyingSymbol, t.WarrantType, t.ExpiryDate, t.StrikePrice, t.ExerciseRatio)
	if err != nil {
		return err
	}
	result, err := tx.Exec(`INSERT INTO transactions(symbol,type,quantity,price,fee,traded_at,note) VALUES(?,?,?,?,?,?,?)`, t.Symbol, t.Type, t.Quantity, t.Price, t.Fee, t.TradedAt, t.Note)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	t.ID, _ = result.LastInsertId()
	_ = a.recordSnapshot()
	return nil
}
