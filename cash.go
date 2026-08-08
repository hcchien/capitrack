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

type cashAccount struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Currency       string  `json:"currency"`
	InitialBalance float64 `json:"initialBalance"`
	Balance        float64 `json:"balance"`
	BalanceBase    float64 `json:"balanceBase"`
	Note           string  `json:"note"`
	CreatedAt      string  `json:"createdAt,omitempty"`
}
type cashTransaction struct {
	ID        int64   `json:"id"`
	AccountID int64   `json:"accountId"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Amount    float64 `json:"amount"`
	Currency  string  `json:"currency"`
	TradedAt  string  `json:"tradedAt"`
	Note      string  `json:"note"`
	CreatedAt string  `json:"createdAt,omitempty"`
}

func (a *app) calculateCashAccounts(base string, usdTwd float64) ([]cashAccount, error) {
	rows, err := a.db.Query(`SELECT c.id,c.name,c.currency,c.initial_balance,c.note,c.created_at,
		c.initial_balance+COALESCE(SUM(CASE WHEN t.type IN ('deposit','dividend','interest','adjustment') THEN t.amount WHEN t.type IN ('withdrawal','fee') THEN -t.amount ELSE 0 END),0)
		FROM cash_accounts c LEFT JOIN cash_transactions t ON t.account_id=c.id GROUP BY c.id ORDER BY c.created_at DESC,c.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []cashAccount{}
	for rows.Next() {
		var x cashAccount
		if err := rows.Scan(&x.ID, &x.Name, &x.Currency, &x.InitialBalance, &x.Note, &x.CreatedAt, &x.Balance); err != nil {
			return nil, err
		}
		x.BalanceBase = convertCurrency(x.Balance, x.Currency, base, usdTwd)
		items = append(items, x)
	}
	return items, rows.Err()
}
func validateCashAccount(x *cashAccount) error {
	x.Name = strings.TrimSpace(x.Name)
	x.Currency = strings.ToUpper(strings.TrimSpace(x.Currency))
	x.Note = strings.TrimSpace(x.Note)
	if x.Name == "" {
		return errors.New("請填寫現金帳戶名稱")
	}
	if x.Currency != "TWD" && x.Currency != "USD" {
		return errors.New("現金帳戶幣別必須是 TWD 或 USD")
	}
	if x.InitialBalance < 0 {
		return errors.New("期初餘額不能小於零")
	}
	return nil
}
func validateCashTransaction(x *cashTransaction) error {
	x.Type = strings.ToLower(strings.TrimSpace(x.Type))
	x.Note = strings.TrimSpace(x.Note)
	valid := map[string]bool{"deposit": true, "withdrawal": true, "dividend": true, "interest": true, "fee": true, "adjustment": true}
	if x.AccountID <= 0 || !valid[x.Type] || x.Amount <= 0 {
		return errors.New("現金流水內容不正確")
	}
	if _, err := time.Parse("2006-01-02", x.TradedAt); err != nil {
		return errors.New("流水日期不正確")
	}
	return nil
}
func (a *app) listCashAccounts(w http.ResponseWriter, r *http.Request) {
	base, rate, _, err := a.portfolioSettings()
	if err == nil {
		var items []cashAccount
		items, err = a.calculateCashAccounts(base, rate)
		if err == nil {
			writeJSON(w, items)
			return
		}
	}
	fail(w, err, 500)
}
func (a *app) createCashAccount(w http.ResponseWriter, r *http.Request) {
	var x cashAccount
	if err := json.NewDecoder(r.Body).Decode(&x); err != nil {
		fail(w, err, 400)
		return
	}
	if err := validateCashAccount(&x); err != nil {
		fail(w, err, 400)
		return
	}
	result, err := a.db.Exec(`INSERT INTO cash_accounts(name,currency,initial_balance,note) VALUES(?,?,?,?)`, x.Name, x.Currency, x.InitialBalance, x.Note)
	if err != nil {
		fail(w, err, 500)
		return
	}
	x.ID, _ = result.LastInsertId()
	_ = a.recordSnapshot()
	writeJSONStatus(w, x, 201)
}
func (a *app) deleteCashAccount(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, err, 400)
		return
	}
	result, err := a.db.Exec(`DELETE FROM cash_accounts WHERE id=?`, id)
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
func (a *app) queryCashTransactions() ([]cashTransaction, error) {
	rows, err := a.db.Query(`SELECT t.id,t.account_id,c.name,t.type,t.amount,c.currency,t.traded_at,t.note,t.created_at FROM cash_transactions t JOIN cash_accounts c ON c.id=t.account_id ORDER BY t.traded_at DESC,t.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []cashTransaction{}
	for rows.Next() {
		var x cashTransaction
		if err := rows.Scan(&x.ID, &x.AccountID, &x.Name, &x.Type, &x.Amount, &x.Currency, &x.TradedAt, &x.Note, &x.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, x)
	}
	return items, rows.Err()
}
func (a *app) listCashTransactions(w http.ResponseWriter, r *http.Request) {
	items, err := a.queryCashTransactions()
	if err != nil {
		fail(w, err, 500)
		return
	}
	writeJSON(w, items)
}
func (a *app) createCashTransaction(w http.ResponseWriter, r *http.Request) {
	var x cashTransaction
	if err := json.NewDecoder(r.Body).Decode(&x); err != nil {
		fail(w, err, 400)
		return
	}
	if err := validateCashTransaction(&x); err != nil {
		fail(w, err, 400)
		return
	}
	result, err := a.db.Exec(`INSERT INTO cash_transactions(account_id,type,amount,traded_at,note) VALUES(?,?,?,?,?)`, x.AccountID, x.Type, x.Amount, x.TradedAt, x.Note)
	if err != nil {
		fail(w, err, 400)
		return
	}
	x.ID, _ = result.LastInsertId()
	_ = a.recordSnapshot()
	writeJSONStatus(w, x, 201)
}
func (a *app) deleteCashTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, err, 400)
		return
	}
	result, err := a.db.Exec(`DELETE FROM cash_transactions WHERE id=?`, id)
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
