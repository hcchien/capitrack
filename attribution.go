package main

import (
	"database/sql"
	"net/http"
	"sort"
	"strings"
	"time"
)

type assetPerformance struct {
	Symbol string
	Name   string
	PnLTWD float64
}
type attributionItem struct {
	Symbol       string  `json:"symbol"`
	Name         string  `json:"name"`
	Contribution float64 `json:"contribution"`
	Percentage   float64 `json:"percentage"`
}
type attributionResult struct {
	Currency     string            `json:"currency"`
	StartAt      string            `json:"startAt"`
	EndAt        string            `json:"endAt"`
	TotalChange  float64           `json:"totalChange"`
	ProfitChange float64           `json:"profitChange"`
	NetFlow      float64           `json:"netFlow"`
	Items        []attributionItem `json:"items"`
}

func (a *app) assetPerformanceTWD(usdTwd float64) ([]assetPerformance, error) {
	txs, err := a.queryTransactions("", "", "")
	if err != nil {
		return nil, err
	}
	type state struct {
		name, currency                  string
		quantity, cost, realized, price float64
		multiplier                      float64
	}
	states := map[string]*state{}
	rows, err := a.db.Query(`SELECT symbol,name,currency,current_price,asset_type,exercise_ratio FROM assets`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var symbol, assetType string
		var s state
		var ratio float64
		if err := rows.Scan(&symbol, &s.name, &s.currency, &s.price, &assetType, &ratio); err != nil {
			rows.Close()
			return nil, err
		}
		s.multiplier = 1
		if assetType == "warrant" && ratio > 0 {
			s.multiplier = ratio
		}
		states[symbol] = &s
	}
	rows.Close()
	for _, t := range txs {
		s := states[t.Symbol]
		if s == nil {
			s = &state{name: t.Name, currency: t.Currency, multiplier: 1}
			states[t.Symbol] = s
		}
		action := t.PositionAction
		if action == "" {
			action = t.Type + "_open"
		}
		if action == "sell_open" {
			s.cost -= t.Quantity*t.Price*s.multiplier - t.Fee
			s.quantity -= t.Quantity
		} else if action == "buy_close" {
			covered := min(t.Quantity, -s.quantity)
			avg := 0.0
			if s.quantity < 0 {
				avg = s.cost / s.quantity
			}
			s.realized += covered*avg - covered*t.Price*s.multiplier - t.Fee
			s.cost += covered * avg
			s.quantity += covered
			if s.quantity > -.00000001 {
				s.quantity, s.cost = 0, 0
			}
		} else if action == "buy_open" {
			s.cost += t.Quantity*t.Price*s.multiplier + t.Fee
			s.quantity += t.Quantity
		} else {
			avg := 0.0
			if s.quantity > 0 {
				avg = s.cost / s.quantity
			}
			sold := min(t.Quantity, s.quantity)
			s.realized += sold*t.Price*s.multiplier - t.Fee - sold*avg
			s.cost -= sold * avg
			s.quantity -= sold
			if s.quantity < .00000001 {
				s.quantity = 0
				s.cost = 0
			}
		}
	}
	result := []assetPerformance{}
	for symbol, s := range states {
		pnl := s.realized + s.quantity*s.price*s.multiplier - s.cost
		if s.currency == "USD" {
			pnl *= usdTwd
		}
		result = append(result, assetPerformance{Symbol: symbol, Name: s.name, PnLTWD: pnl})
	}
	return result, nil
}

func (a *app) portfolioAttribution(w http.ResponseWriter, r *http.Request) {
	result, err := a.calculateAttribution(r.URL.Query().Get("range"))
	if err != nil {
		fail(w, err, 500)
		return
	}
	writeJSON(w, result)
}

func (a *app) calculateAttribution(requestedRange string) (attributionResult, error) {
	rangeName := strings.ToUpper(requestedRange)
	days := map[string]int{"1M": 30, "3M": 90, "1Y": 365}
	var startID, endID int64
	var startAt, endAt string
	var startTotal, endTotal float64
	eligible := `EXISTS(SELECT 1 FROM asset_performance_snapshots aps WHERE aps.snapshot_id=portfolio_snapshots.id)`
	if err := a.db.QueryRow(`SELECT id,captured_at,total_twd FROM portfolio_snapshots WHERE `+eligible+` ORDER BY captured_at DESC,id DESC LIMIT 1`).Scan(&endID, &endAt, &endTotal); err == sql.ErrNoRows {
		return attributionResult{Items: []attributionItem{}}, nil
	} else if err != nil {
		return attributionResult{}, err
	}
	if dayCount, ok := days[rangeName]; ok {
		cutoff := time.Now().AddDate(0, 0, -dayCount).Format(time.RFC3339)
		err := a.db.QueryRow(`SELECT id,captured_at,total_twd FROM portfolio_snapshots WHERE captured_at<=? AND `+eligible+` ORDER BY captured_at DESC,id DESC LIMIT 1`, cutoff).Scan(&startID, &startAt, &startTotal)
		if err == sql.ErrNoRows {
			err = a.db.QueryRow(`SELECT id,captured_at,total_twd FROM portfolio_snapshots WHERE `+eligible+` ORDER BY captured_at ASC,id ASC LIMIT 1`).Scan(&startID, &startAt, &startTotal)
		}
		if err != nil {
			return attributionResult{}, err
		}
	} else {
		if err := a.db.QueryRow(`SELECT id,captured_at,total_twd FROM portfolio_snapshots WHERE `+eligible+` ORDER BY captured_at ASC,id ASC LIMIT 1`).Scan(&startID, &startAt, &startTotal); err != nil {
			return attributionResult{}, err
		}
	}
	startPnL, err := a.snapshotPnL(startID)
	if err != nil {
		return attributionResult{}, err
	}
	endPnL, err := a.snapshotPnL(endID)
	if err != nil {
		return attributionResult{}, err
	}
	all := map[string]attributionItem{}
	for symbol, item := range endPnL {
		entry := attributionItem{Symbol: symbol, Name: item.Name, Contribution: item.PnLTWD - startPnL[symbol].PnLTWD}
		all[symbol] = entry
	}
	for symbol, item := range startPnL {
		if _, ok := all[symbol]; !ok {
			all[symbol] = attributionItem{Symbol: symbol, Name: item.Name, Contribution: -item.PnLTWD}
		}
	}
	profitChange := 0.0
	items := make([]attributionItem, 0, len(all))
	for _, item := range all {
		profitChange += item.Contribution
		items = append(items, item)
	}
	if profitChange != 0 {
		for i := range items {
			items[i].Percentage = items[i].Contribution / profitChange * 100
		}
	}
	sort.Slice(items, func(i, j int) bool { return abs(items[i].Contribution) > abs(items[j].Contribution) })
	base, rate, _, err := a.portfolioSettings()
	if err != nil {
		return attributionResult{}, err
	}
	factor := 1.0
	if base == "USD" && rate > 0 {
		factor = 1 / rate
	}
	for i := range items {
		items[i].Contribution *= factor
	}
	assetChange := (endTotal - startTotal) * factor
	profitChange *= factor
	return attributionResult{Currency: base, StartAt: startAt, EndAt: endAt, TotalChange: assetChange, ProfitChange: profitChange, NetFlow: assetChange - profitChange, Items: items}, nil
}

func (a *app) snapshotPnL(snapshotID int64) (map[string]assetPerformance, error) {
	rows, err := a.db.Query(`SELECT symbol,name,pnl_twd FROM asset_performance_snapshots WHERE snapshot_id=?`, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]assetPerformance{}
	for rows.Next() {
		var item assetPerformance
		if err := rows.Scan(&item.Symbol, &item.Name, &item.PnLTWD); err != nil {
			return nil, err
		}
		result[item.Symbol] = item
	}
	return result, rows.Err()
}
func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
