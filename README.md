# CapiTrack

一個在本機執行的投資組合紀錄工具。交易資料儲存在 SQLite，新增、編輯或刪除交易後，持股數量、成本、總值與損益會自動重算。

## 啟動

需要 Go 1.25 或更新版本：

```bash
go mod download
go run .
```

接著開啟 <http://localhost:8080>。資料庫位於 `data/capitrack.db`；備份這個檔案即可保存全部資料。

也可使用 `PORT` 改變連接埠，或用 `CAPITRACK_DATA_DIR` 指定資料目錄。

## 已有功能

- 持股總值、投入成本、未實現與已實現損益
- 依交易紀錄自動計算持股與加權平均成本
- 新增、編輯與刪除買賣紀錄
- 手動更新各標的目前價格
- 依投資標的及日期區間篩選交易
- 以交易備註回顧當時的投資決策
- 台股與美股 ticker／公司名稱 autocomplete
- 每個標的以 TWD 或 USD 保存交易與行情
- 總值可切換用 TWD 或 USD 顯示
- 一鍵更新持股最新價格與 USD/TWD 匯率
- 總資產互動走勢圖，支援 7 天、1 個月、3 個月與全部期間
- 績效歸因：拆分各標的損益貢獻、總投資損益與淨投入／提出

總資產走勢從啟用此版本後開始累積。新增或修改交易、更新價格及同步最新行情時，系統都會在 SQLite 保存一筆資產快照；滑鼠移動或觸控圖表可查看各時間點的精確總值。

績效歸因使用每個 snapshot 的標的累積損益做期初、期末比較，因此新增投資本金不會被誤算為獲利。美股損益以各時間點 USD/TWD 匯率換算，匯率變化也會反映在該標的的貢獻中。

## 行情資料

- 台股清單與名稱：臺灣證券交易所、證券櫃檯買賣中心公開資料
- 台股價格：臺灣證券交易所 MIS
- 美股名稱與價格：Nasdaq 公開行情 API
- USD/TWD：ExchangeRate-API 的免金鑰匯率端點

行情更新需要網路連線；若個別標的暫時無法取得，其他標的仍會繼續更新，畫面會顯示失敗數量。

## MCP

CapiTrack 使用官方 Go SDK，提供九個 MCP tools：

- `get_portfolio`：讀取目前持股、總值、成本與損益
- `get_portfolio_history`：讀取指定期間的總資產走勢
- `get_performance_attribution`：讀取總資產變化、投資損益、淨投入與各標的貢獻
- `list_transactions`：依標的或日期區間查詢交易
- `search_assets`：依 ticker 或公司名稱搜尋台股、美股
- `add_transaction`：新增買入或賣出交易
- `set_base_currency`：設定總值、走勢與績效歸因的顯示幣別
- `update_current_price`：更新標的目前市價
- `refresh_market_data`：更新全部持股價格與 USD/TWD 匯率

網站啟動後，Streamable HTTP MCP endpoint 位於：

```text
http://localhost:8080/mcp
```

若 MCP 客戶端使用 stdio transport，可將啟動指令設為：

```bash
go run . mcp
```

兩種 MCP transport 都會使用網站相同的 `data/capitrack.db`。
