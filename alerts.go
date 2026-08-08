package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type priceAlert struct {
	ID            int64   `json:"id"`
	Symbol        string  `json:"symbol"`
	Name          string  `json:"name"`
	Market        string  `json:"market"`
	RuleType      string  `json:"ruleType"`
	Value         float64 `json:"value"`
	AnchorPrice   float64 `json:"anchorPrice"`
	HighWatermark float64 `json:"highWatermark"`
	TriggerPrice  float64 `json:"triggerPrice"`
	Active        bool    `json:"active"`
	OneShot       bool    `json:"oneShot"`
	LastPrice     float64 `json:"lastPrice"`
	Currency      string  `json:"currency"`
	CreatedAt     string  `json:"createdAt"`
}
type alertEvent struct {
	ID           int64   `json:"id"`
	AlertID      int64   `json:"alertId"`
	Symbol       string  `json:"symbol"`
	Name         string  `json:"name"`
	RuleType     string  `json:"ruleType"`
	TriggerPrice float64 `json:"triggerPrice"`
	MarketPrice  float64 `json:"marketPrice"`
	TriggeredAt  string  `json:"triggeredAt"`
	IsRead       bool    `json:"isRead"`
	Currency     string  `json:"currency"`
}
type alertInput struct {
	Symbol   string  `json:"symbol"`
	RuleType string  `json:"ruleType"`
	Value    float64 `json:"value"`
	OneShot  bool    `json:"oneShot"`
}

func (a *app) listAlerts(w http.ResponseWriter, r *http.Request) {
	items, err := a.getAlerts()
	if err != nil {
		fail(w, err, 500)
		return
	}
	writeJSON(w, items)
}
func (a *app) getAlerts() ([]priceAlert, error) {
	rows, err := a.db.Query(`SELECT p.id,p.symbol,a.name,a.market,p.rule_type,p.value,p.anchor_price,p.high_watermark,p.trigger_price,p.active,p.one_shot,p.last_price,a.currency,p.created_at FROM price_alerts p JOIN assets a ON a.symbol=p.symbol ORDER BY p.active DESC,p.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []priceAlert{}
	for rows.Next() {
		var item priceAlert
		if err := rows.Scan(&item.ID, &item.Symbol, &item.Name, &item.Market, &item.RuleType, &item.Value, &item.AnchorPrice, &item.HighWatermark, &item.TriggerPrice, &item.Active, &item.OneShot, &item.LastPrice, &item.Currency, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (a *app) createAlert(w http.ResponseWriter, r *http.Request) {
	var input alertInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		fail(w, err, 400)
		return
	}
	item, err := a.addAlert(input)
	if err != nil {
		fail(w, err, 400)
		return
	}
	writeJSONStatus(w, item, 201)
}
func (a *app) addAlert(input alertInput) (priceAlert, error) {
	input.Symbol = strings.ToUpper(strings.TrimSpace(input.Symbol))
	input.RuleType = strings.ToLower(input.RuleType)
	valid := map[string]bool{"price_below": true, "price_above": true, "cost_loss_pct": true, "cost_profit_pct": true, "trailing_stop_pct": true}
	if !valid[input.RuleType] || input.Value <= 0 {
		return priceAlert{}, errors.New("提醒條件或數值不正確")
	}
	var name, currency string
	var current float64
	if err := a.db.QueryRow(`SELECT name,currency,current_price FROM assets WHERE symbol=?`, input.Symbol).Scan(&name, &currency, &current); err != nil {
		return priceAlert{}, errors.New("找不到投資標的")
	}
	avg := a.averageCost(input.Symbol)
	anchor := current
	if strings.HasPrefix(input.RuleType, "cost_") {
		anchor = avg
	}
	if anchor <= 0 {
		return priceAlert{}, errors.New("缺少目前價格或持有成本")
	}
	trigger := input.Value
	if input.RuleType == "cost_loss_pct" || input.RuleType == "trailing_stop_pct" {
		trigger = anchor * (1 - input.Value/100)
	} else if input.RuleType == "cost_profit_pct" {
		trigger = anchor * (1 + input.Value/100)
	}
	result, err := a.db.Exec(`INSERT INTO price_alerts(symbol,rule_type,value,anchor_price,high_watermark,trigger_price,active,one_shot,last_price) VALUES(?,?,?,?,?,?,1,?,?)`, input.Symbol, input.RuleType, input.Value, anchor, anchor, trigger, input.OneShot, current)
	if err != nil {
		return priceAlert{}, err
	}
	id, _ := result.LastInsertId()
	return priceAlert{ID: id, Symbol: input.Symbol, Name: name, Currency: currency, RuleType: input.RuleType, Value: input.Value, AnchorPrice: anchor, HighWatermark: anchor, TriggerPrice: trigger, Active: true, OneShot: input.OneShot, LastPrice: current}, nil
}
func (a *app) averageCost(symbol string) float64 {
	result, err := a.calculatePortfolio()
	if err != nil {
		return 0
	}
	for _, h := range result.Holdings {
		if h.Symbol == symbol {
			return h.AverageCost
		}
	}
	return 0
}
func (a *app) updateAlert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, err, 400)
		return
	}
	var body struct {
		Active bool `json:"active"`
	}
	if err = json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, err, 400)
		return
	}
	result, err := a.db.Exec(`UPDATE price_alerts SET active=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, body.Active, id)
	if err != nil {
		fail(w, err, 500)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, sql.ErrNoRows, 404)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
func (a *app) deleteAlert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, err, 400)
		return
	}
	if _, err = a.db.Exec(`DELETE FROM price_alerts WHERE id=?`, id); err != nil {
		fail(w, err, 500)
		return
	}
	w.WriteHeader(204)
}
func (a *app) listAlertEvents(w http.ResponseWriter, r *http.Request) {
	items, err := a.getAlertEvents()
	if err != nil {
		fail(w, err, 500)
		return
	}
	writeJSON(w, items)
}
func (a *app) getAlertEvents() ([]alertEvent, error) {
	rows, err := a.db.Query(`SELECT e.id,e.alert_id,e.symbol,a.name,e.rule_type,e.trigger_price,e.market_price,e.triggered_at,e.is_read,a.currency FROM alert_events e JOIN assets a ON a.symbol=e.symbol ORDER BY e.triggered_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []alertEvent{}
	for rows.Next() {
		var item alertEvent
		if err := rows.Scan(&item.ID, &item.AlertID, &item.Symbol, &item.Name, &item.RuleType, &item.TriggerPrice, &item.MarketPrice, &item.TriggeredAt, &item.IsRead, &item.Currency); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (a *app) readAlertEvent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, err, 400)
		return
	}
	_, err = a.db.Exec(`UPDATE alert_events SET is_read=1 WHERE id=?`, id)
	if err != nil {
		fail(w, err, 500)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
func (a *app) checkAlertsHTTP(w http.ResponseWriter, r *http.Request) {
	count, err := a.checkAlerts(r.Context(), false)
	if err != nil {
		fail(w, err, 500)
		return
	}
	writeJSON(w, map[string]any{"triggered": count})
}

func (a *app) checkAlerts(ctx context.Context, fetch bool) (int, error) {
	items, err := a.getAlerts()
	if err != nil {
		return 0, err
	}
	prices := map[string]float64{}
	if fetch {
		for _, item := range items {
			if !item.Active {
				continue
			}
			if _, ok := prices[item.Symbol]; ok {
				continue
			}
			lookup := item.Symbol
			if item.Market == "TW" && !strings.Contains(lookup, ".") {
				lookup += ".TW"
			}
			quote, err := a.market.Quote(ctx, lookup)
			if err == nil {
				prices[item.Symbol] = quote.Price
				_, _ = a.db.Exec(`UPDATE assets SET current_price=?,updated_at=CURRENT_TIMESTAMP WHERE symbol=?`, quote.Price, item.Symbol)
			}
		}
	}
	triggered := 0
	for _, item := range items {
		if !item.Active {
			continue
		}
		price := prices[item.Symbol]
		if price == 0 {
			_ = a.db.QueryRow(`SELECT current_price FROM assets WHERE symbol=?`, item.Symbol).Scan(&price)
		}
		high := item.HighWatermark
		target := item.TriggerPrice
		if item.RuleType == "trailing_stop_pct" && price > high {
			high = price
			target = high * (1 - item.Value/100)
		}
		hit := (item.RuleType == "price_below" || item.RuleType == "cost_loss_pct" || item.RuleType == "trailing_stop_pct") && price <= target || (item.RuleType == "price_above" || item.RuleType == "cost_profit_pct") && price >= target
		active := item.Active
		if hit && price > 0 {
			triggered++
			_, _ = a.db.Exec(`INSERT INTO alert_events(alert_id,symbol,rule_type,trigger_price,market_price,triggered_at) VALUES(?,?,?,?,?,?)`, item.ID, item.Symbol, item.RuleType, target, price, time.Now().Format(time.RFC3339))
			if item.OneShot {
				active = false
			}
		}
		_, err = a.db.Exec(`UPDATE price_alerts SET high_watermark=?,trigger_price=?,last_price=?,active=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, high, target, price, active, item.ID)
		if err != nil {
			return triggered, err
		}
	}
	return triggered, nil
}
func (a *app) runAlertMonitor(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if count, err := a.checkAlerts(ctx, true); err != nil {
				fmt.Printf("alert monitor: %v\n", err)
			} else if count > 0 {
				fmt.Printf("price alerts triggered: %d\n", count)
			}
		}
	}
}
