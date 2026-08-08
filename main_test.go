package main

import (
	"context"
	"database/sql"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "modernc.org/sqlite"
)

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
	defer db.Close()
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := (&app{db: db}).newMCPServer().Connect(ctx, serverTransport, nil)
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
