package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"

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
}

type priceInput struct {
	Symbol string  `json:"symbol" jsonschema:"要更新的投資標的代號"`
	Price  float64 `json:"price" jsonschema:"目前每單位市價"`
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
	server := mcp.NewServer(&mcp.Implementation{Name: "capitrack", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_portfolio", Description: "取得目前投資組合，包括持股、總市值、成本、配置比例與已實現／未實現損益。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, portfolioResult, error) {
		result, err := a.calculatePortfolio()
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
		Name: "add_transaction", Description: "新增一筆買入或賣出交易；持股、成本與損益會自動重新計算。",
		Annotations: &mcp.ToolAnnotations{},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input transactionInput) (*mcp.CallToolResult, mutationOutput, error) {
		t := transaction{Symbol: input.Symbol, Name: input.Name, Type: input.Type, Quantity: input.Quantity, Price: input.Price, Fee: input.Fee, TradedAt: input.TradedAt, Note: input.Note}
		if err := a.insertTransaction(&t); err != nil {
			return nil, mutationOutput{}, err
		}
		return nil, mutationOutput{Success: true, Message: "交易已新增", Trade: t}, nil
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
		return nil, mutationOutput{Success: true, Message: "目前價格已更新"}, nil
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
	_, err = tx.Exec(`INSERT INTO assets(symbol,name,current_price) VALUES(?,?,?) ON CONFLICT(symbol) DO UPDATE SET name=excluded.name`, t.Symbol, t.Name, t.Price)
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
	return nil
}
