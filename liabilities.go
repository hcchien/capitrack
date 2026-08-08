package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type liability struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Category       string  `json:"category"`
	Currency       string  `json:"currency"`
	InitialBalance float64 `json:"initialBalance"`
	Balance        float64 `json:"balance"`
	BalanceBase    float64 `json:"balanceBase"`
	InterestRate   float64 `json:"interestRate"`
	Note           string  `json:"note"`
	CreatedAt      string  `json:"createdAt,omitempty"`
}

type liabilityTransaction struct {
	ID          int64   `json:"id"`
	LiabilityID int64   `json:"liabilityId"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	TradedAt    string  `json:"tradedAt"`
	Note        string  `json:"note"`
	CreatedAt   string  `json:"createdAt,omitempty"`
}

func convertCurrency(value float64, from, to string, usdTwd float64) float64 {
	if from == to {
		return value
	}
	if from == "USD" && to == "TWD" {
		return value * usdTwd
	}
	if from == "TWD" && to == "USD" && usdTwd > 0 {
		return value / usdTwd
	}
	return value
}

func (a *app) calculateLiabilities(base string, usdTwd float64) ([]liability, error) {
	rows, err := a.db.Query(`SELECT l.id,l.name,l.category,l.currency,l.initial_balance,l.interest_rate,l.note,l.created_at,
		l.initial_balance+COALESCE(SUM(CASE WHEN t.type IN ('borrow','interest','adjustment') THEN t.amount WHEN t.type='repay' THEN -t.amount ELSE 0 END),0)
		FROM liabilities l LEFT JOIN liability_transactions t ON t.liability_id=l.id GROUP BY l.id ORDER BY l.created_at DESC,l.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []liability{}
	for rows.Next() {
		var item liability
		if err := rows.Scan(&item.ID, &item.Name, &item.Category, &item.Currency, &item.InitialBalance, &item.InterestRate, &item.Note, &item.CreatedAt, &item.Balance); err != nil {
			return nil, err
		}
		if item.Balance < 0 {
			item.Balance = 0
		}
		item.BalanceBase = convertCurrency(item.Balance, item.Currency, base, usdTwd)
		items = append(items, item)
	}
	return items, rows.Err()
}

func validateLiability(item *liability) error {
	item.Name = strings.TrimSpace(item.Name)
	item.Category = strings.ToLower(strings.TrimSpace(item.Category))
	item.Currency = strings.ToUpper(strings.TrimSpace(item.Currency))
	item.Note = strings.TrimSpace(item.Note)
	if item.Category == "" {
		item.Category = "other"
	}
	if item.Currency == "" {
		item.Currency = "TWD"
	}
	if item.Name == "" {
		return errors.New("請填寫負債名稱")
	}
	if item.Currency != "TWD" && item.Currency != "USD" {
		return errors.New("負債幣別必須是 TWD 或 USD")
	}
	if item.InitialBalance < 0 || item.InterestRate < 0 {
		return errors.New("負債金額或利率不正確")
	}
	return nil
}

func (a *app) listLiabilities(w http.ResponseWriter, r *http.Request) {
	base, rate, _, err := a.portfolioSettings()
	if err != nil {
		fail(w, err, 500)
		return
	}
	items, err := a.calculateLiabilities(base, rate)
	if err != nil {
		fail(w, err, 500)
		return
	}
	writeJSON(w, items)
}
func (a *app) createLiability(w http.ResponseWriter, r *http.Request) {
	var item liability
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		fail(w, err, 400)
		return
	}
	if err := validateLiability(&item); err != nil {
		fail(w, err, 400)
		return
	}
	result, err := a.db.Exec(`INSERT INTO liabilities(name,category,currency,initial_balance,interest_rate,note) VALUES(?,?,?,?,?,?)`, item.Name, item.Category, item.Currency, item.InitialBalance, item.InterestRate, item.Note)
	if err != nil {
		fail(w, err, 500)
		return
	}
	item.ID, _ = result.LastInsertId()
	_ = a.recordSnapshot()
	writeJSONStatus(w, item, 201)
}
func (a *app) updateLiability(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, err, 400)
		return
	}
	var item liability
	if err = json.NewDecoder(r.Body).Decode(&item); err != nil {
		fail(w, err, 400)
		return
	}
	if err = validateLiability(&item); err != nil {
		fail(w, err, 400)
		return
	}
	result, err := a.db.Exec(`UPDATE liabilities SET name=?,category=?,currency=?,initial_balance=?,interest_rate=?,note=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, item.Name, item.Category, item.Currency, item.InitialBalance, item.InterestRate, item.Note, id)
	if err != nil {
		fail(w, err, 500)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, sql.ErrNoRows, 404)
		return
	}
	item.ID = id
	_ = a.recordSnapshot()
	writeJSON(w, item)
}
func (a *app) deleteLiability(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, err, 400)
		return
	}
	result, err := a.db.Exec(`DELETE FROM liabilities WHERE id=?`, id)
	if err != nil {
		fail(w, err, 500)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, sql.ErrNoRows, 404)
		return
	}
	_ = a.recordSnapshot()
	w.WriteHeader(204)
}

func (a *app) queryLiabilityTransactions() ([]liabilityTransaction, error) {
	rows, err := a.db.Query(`SELECT t.id,t.liability_id,l.name,t.type,t.amount,l.currency,t.traded_at,t.note,t.created_at FROM liability_transactions t JOIN liabilities l ON l.id=t.liability_id ORDER BY t.traded_at DESC,t.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []liabilityTransaction{}
	for rows.Next() {
		var item liabilityTransaction
		if err := rows.Scan(&item.ID, &item.LiabilityID, &item.Name, &item.Type, &item.Amount, &item.Currency, &item.TradedAt, &item.Note, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func validateLiabilityTransaction(item *liabilityTransaction) error {
	item.Type = strings.ToLower(strings.TrimSpace(item.Type))
	item.Note = strings.TrimSpace(item.Note)
	valid := map[string]bool{"borrow": true, "repay": true, "interest": true, "adjustment": true}
	if item.LiabilityID <= 0 || !valid[item.Type] || item.Amount <= 0 {
		return errors.New("負債異動內容不正確")
	}
	if _, err := time.Parse("2006-01-02", item.TradedAt); err != nil {
		return errors.New("異動日期不正確")
	}
	return nil
}
func (a *app) listLiabilityTransactions(w http.ResponseWriter, r *http.Request) {
	items, err := a.queryLiabilityTransactions()
	if err != nil {
		fail(w, err, 500)
		return
	}
	writeJSON(w, items)
}
func (a *app) createLiabilityTransaction(w http.ResponseWriter, r *http.Request) {
	var item liabilityTransaction
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		fail(w, err, 400)
		return
	}
	if err := validateLiabilityTransaction(&item); err != nil {
		fail(w, err, 400)
		return
	}
	result, err := a.db.Exec(`INSERT INTO liability_transactions(liability_id,type,amount,traded_at,note) VALUES(?,?,?,?,?)`, item.LiabilityID, item.Type, item.Amount, item.TradedAt, item.Note)
	if err != nil {
		fail(w, err, 400)
		return
	}
	item.ID, _ = result.LastInsertId()
	_ = a.recordSnapshot()
	writeJSONStatus(w, item, 201)
}
func (a *app) deleteLiabilityTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, err, 400)
		return
	}
	result, err := a.db.Exec(`DELETE FROM liability_transactions WHERE id=?`, id)
	if err != nil {
		fail(w, err, 500)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, sql.ErrNoRows, 404)
		return
	}
	_ = a.recordSnapshot()
	w.WriteHeader(204)
}
