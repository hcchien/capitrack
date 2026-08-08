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
	Symbol   string  `json:"symbol" jsonschema:"投資標的代號，例如 2330 或 AAPL"`
	Name     string  `json:"name" jsonschema:"投資標的名稱"`
	Type     string  `json:"type" jsonschema:"交易類型，只能是 buy 或 sell"`
	Quantity float64 `json:"quantity" jsonschema:"交易數量，必須大於 0"`
	Price    float64 `json:"price" jsonschema:"每單位成交價格"`
	Fee      float64 `json:"fee,omitempty" jsonschema:"交易手續費"`
	TradedAt string  `json:"tradedAt" jsonschema:"交易日期，格式 YYYY-MM-DD"`
	Note     string  `json:"note,omitempty" jsonschema:"交易理由或備註"`
	Market   string  `json:"market,omitempty" jsonschema:"市場，只能是 TW 或 US；預設 TW"`
	Currency string  `json:"currency,omitempty" jsonschema:"交易幣別，只能是 TWD 或 USD；依市場預設"`
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
		Name: "get_portfolio_history", Description: "取得總資產變化走勢的時間序列，可指定 7D、1M、3M 或 ALL。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input rangeInput) (*mcp.CallToolResult, portfolioHistoryResult, error) {
		result, err := a.getPortfolioHistory(input.Range)
		return nil, result, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_performance_attribution", Description: "取得指定期間的績效歸因，包括總資產變化、投資損益、淨投入／提出，以及各標的損益貢獻。",
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
		t := transaction{Symbol: input.Symbol, Name: input.Name, Type: input.Type, Quantity: input.Quantity, Price: input.Price, Fee: input.Fee, TradedAt: input.TradedAt, Note: input.Note, Market: input.Market, Currency: input.Currency}
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
		return nil, mutationOutput{Success: true, Message: fmt.Sprintf("已更新 %d 個投資標的", updated)}, rows.Err()
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
	_, err = tx.Exec(`INSERT INTO assets(symbol,name,current_price,market,currency) VALUES(?,?,?,?,?) ON CONFLICT(symbol) DO UPDATE SET name=excluded.name,market=excluded.market,currency=excluded.currency`, t.Symbol, t.Name, t.Price, t.Market, t.Currency)
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
