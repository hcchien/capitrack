package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "modernc.org/sqlite"
)

type app struct{ db *sql.DB }

type transaction struct {
	ID        int64   `json:"id"`
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Quantity  float64 `json:"quantity"`
	Price     float64 `json:"price"`
	Fee       float64 `json:"fee"`
	TradedAt  string  `json:"tradedAt"`
	Note      string  `json:"note"`
	CreatedAt string  `json:"createdAt,omitempty"`
}

type holding struct {
	Symbol       string  `json:"symbol"`
	Name         string  `json:"name"`
	Quantity     float64 `json:"quantity"`
	AverageCost  float64 `json:"averageCost"`
	CurrentPrice float64 `json:"currentPrice"`
	MarketValue  float64 `json:"marketValue"`
	CostBasis    float64 `json:"costBasis"`
	Unrealized   float64 `json:"unrealized"`
	UnrealizedPC float64 `json:"unrealizedPercent"`
	Realized     float64 `json:"realized"`
	Allocation   float64 `json:"allocation"`
}

type portfolioSummary struct {
	TotalValue        float64 `json:"totalValue"`
	TotalCost         float64 `json:"totalCost"`
	Unrealized        float64 `json:"unrealized"`
	UnrealizedPercent float64 `json:"unrealizedPercent"`
	Realized          float64 `json:"realized"`
}

type portfolioResult struct {
	Holdings []holding        `json:"holdings"`
	Summary  portfolioSummary `json:"summary"`
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

	a := &app{db: db}
	mcpServer := a.newMCPServer()
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		if err := mcpServer.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Fatal(err)
		}
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/portfolio", a.portfolio)
	mux.HandleFunc("GET /api/transactions", a.listTransactions)
	mux.HandleFunc("POST /api/transactions", a.createTransaction)
	mux.HandleFunc("PUT /api/transactions/{id}", a.updateTransaction)
	mux.HandleFunc("DELETE /api/transactions/{id}", a.deleteTransaction)
	mux.HandleFunc("PUT /api/assets/{symbol}/price", a.updatePrice)
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
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
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
	prices := map[string]float64{}
	rows, err := a.db.Query(`SELECT symbol, current_price FROM assets`)
	if err != nil {
		return portfolioResult{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		var p float64
		_ = rows.Scan(&s, &p)
		prices[s] = p
	}

	bySymbol := map[string]*holding{}
	for _, t := range txs {
		h := bySymbol[t.Symbol]
		if h == nil {
			h = &holding{Symbol: t.Symbol, Name: t.Name, CurrentPrice: prices[t.Symbol]}
			bySymbol[t.Symbol] = h
		}
		if t.Type == "buy" {
			h.CostBasis += t.Quantity*t.Price + t.Fee
			h.Quantity += t.Quantity
		} else {
			avg := 0.0
			if h.Quantity > 0 {
				avg = h.CostBasis / h.Quantity
			}
			sold := min(t.Quantity, h.Quantity)
			h.Realized += sold*t.Price - t.Fee - sold*avg
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
		if h.Quantity > 0 {
			h.AverageCost = h.CostBasis / h.Quantity
			h.MarketValue = h.Quantity * h.CurrentPrice
			h.Unrealized = h.MarketValue - h.CostBasis
			if h.CostBasis != 0 {
				h.UnrealizedPC = h.Unrealized / h.CostBasis * 100
			}
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
	return portfolioResult{Holdings: holdings, Summary: portfolioSummary{
		TotalValue: totalValue, TotalCost: totalCost, Unrealized: totalValue - totalCost,
		UnrealizedPercent: percent(totalValue-totalCost, totalCost), Realized: totalRealized,
	}}, rows.Err()
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
	query := `SELECT t.id, t.symbol, a.name, t.type, t.quantity, t.price, t.fee, t.traded_at, t.note, t.created_at
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
		if err := rows.Scan(&t.ID, &t.Symbol, &t.Name, &t.Type, &t.Quantity, &t.Price, &t.Fee, &t.TradedAt, &t.Note, &t.CreatedAt); err != nil {
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
	_, err = tx.Exec(`INSERT INTO assets(symbol,name,current_price) VALUES(?,?,?) ON CONFLICT(symbol) DO UPDATE SET name=excluded.name`, t.Symbol, t.Name, t.Price)
	if err != nil {
		fail(w, err, 500)
		return
	}
	result, err := tx.Exec(`INSERT INTO transactions(symbol,type,quantity,price,fee,traded_at,note) VALUES(?,?,?,?,?,?,?)`, t.Symbol, t.Type, t.Quantity, t.Price, t.Fee, t.TradedAt, t.Note)
	if err != nil {
		fail(w, err, 500)
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, err, 500)
		return
	}
	t.ID, _ = result.LastInsertId()
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
	_, err = tx.Exec(`INSERT INTO assets(symbol,name,current_price) VALUES(?,?,?) ON CONFLICT(symbol) DO UPDATE SET name=excluded.name`, t.Symbol, t.Name, t.Price)
	if err != nil {
		fail(w, err, 500)
		return
	}
	result, err := tx.Exec(`UPDATE transactions SET symbol=?,type=?,quantity=?,price=?,fee=?,traded_at=?,note=? WHERE id=?`, t.Symbol, t.Type, t.Quantity, t.Price, t.Fee, t.TradedAt, t.Note, id)
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
	writeJSON(w, map[string]any{"ok": true})
}

func validate(t *transaction) error {
	t.Symbol = strings.ToUpper(strings.TrimSpace(t.Symbol))
	t.Name = strings.TrimSpace(t.Name)
	t.Type = strings.ToLower(t.Type)
	t.Note = strings.TrimSpace(t.Note)
	if t.Symbol == "" || t.Name == "" {
		return errors.New("請填寫代號與名稱")
	}
	if t.Type != "buy" && t.Type != "sell" {
		return errors.New("交易類型必須是買入或賣出")
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
