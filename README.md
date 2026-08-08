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

停止服務時，在啟動程式的 terminal 按 `Ctrl+C`。這個專案預設只監聽本機；`localhost` 網址不會公開到其他電腦或手機。

## 本機資料與備份

- SQLite 預設位置：`data/capitrack.db`
- `data/`、`*.db`、`*.sqlite` 及 SQLite 的 WAL／SHM 暫存檔都已加入 `.gitignore`
- Git 只保存程式碼，不會保存你的投資交易與提醒資料
- 備份時請先停止服務，再複製 `data/capitrack.db`

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
- 美股權證交易：記錄連結標的、Call／Put、到期日、履約價與行使比例
- 固定價格、成本停損／停利比例，以及只隨新高上調的移動停損提醒
- 瀏覽器系統通知、觸發紀錄與每 5 分鐘背景檢查（本機服務需保持運行）
- 負債紀錄：房貸、貸款、信用卡或其他負債，可記錄借款、利息與還款
- 淨資產總覽與走勢：總資產扣除 TWD／USD 負債後自動換算

總資產走勢從啟用此版本後開始累積。新增或修改交易、更新價格及同步最新行情時，系統都會在 SQLite 保存一筆資產快照；滑鼠移動或觸控圖表可查看各時間點的精確總值。

績效歸因使用每個 snapshot 的標的累積損益做期初、期末比較，因此新增投資本金不會被誤算為獲利。美股損益以各時間點 USD/TWD 匯率換算，匯率變化也會反映在該標的的貢獻中。

## 價格提醒

提醒可以設定為指定價格以下／以上、相對持有成本的停損／停利比例，或移動停損比例。移動停損以設定當下的市價為起點，最高價只會往上更新，因此觸發價不會因行情回落而下修。例如市價 600、移動停損 15% 時，初始觸發價是 510；若之後最高價到 700，觸發價會提高為 595。

網站服務運行時會每 5 分鐘取得行情並檢查提醒。「最新更新」與 MCP 的 `check_price_alerts` 也會檢查提醒。瀏覽器系統通知需由使用者在價格提醒頁面主動允許；即使沒有允許，觸發紀錄仍會保存在 SQLite。

## 美股權證

新增交易時選擇「美股權證」，市場與幣別會固定為 US／USD。除了 ticker 與成交資料，也必須填寫連結標的、Call／Put、到期日、履約價與行使比例。權證和股票共用持股、總值、走勢、績效歸因及提醒功能。

## 負債紀錄

負債可使用 TWD 或 USD，並記錄分類、期初餘額、年利率與備註。之後透過負債異動新增借款、利息或其他增加，也可用還款降低餘額；餘額最低以零計算。投資總覽會分別顯示總資產、總負債與淨資產，資產走勢圖則記錄淨資產變化。

## 行情資料

- 台股清單與名稱：臺灣證券交易所、證券櫃檯買賣中心公開資料
- 台股價格：臺灣證券交易所 MIS
- 美股名稱與價格：Nasdaq 公開行情 API
- USD/TWD：ExchangeRate-API 的免金鑰匯率端點

行情更新需要網路連線；若個別標的暫時無法取得，其他標的仍會繼續更新，畫面會顯示失敗數量。

## MCP

CapiTrack 使用官方 Go SDK，提供十九個 MCP tools：

- `get_portfolio`：讀取目前持股、總值、成本與損益
- `get_portfolio_history`：讀取指定期間的總資產走勢
- `get_performance_attribution`：讀取總資產變化、投資損益、淨投入與各標的貢獻
- `list_transactions`：依標的或日期區間查詢交易
- `search_assets`：依 ticker 或公司名稱搜尋台股、美股
- `add_transaction`：新增買入或賣出交易
- `set_base_currency`：設定總值、走勢與績效歸因的顯示幣別
- `update_current_price`：更新標的目前市價
- `refresh_market_data`：更新全部持股價格與 USD/TWD 匯率
- `list_price_alerts`：列出價格提醒
- `create_price_alert`：建立購入、停損、停利或移動停損提醒
- `set_price_alert_active`：啟用或暫停提醒
- `delete_price_alert`：刪除提醒
- `list_alert_events`：列出提醒觸發紀錄
- `check_price_alerts`：取得最新行情並立即檢查提醒
- `list_liabilities`：列出負債與目前餘額
- `create_liability`：新增負債項目
- `list_liability_transactions`：列出負債異動
- `add_liability_transaction`：新增借款、還款、利息或調整

網站啟動後，Streamable HTTP MCP endpoint 位於：

```text
http://localhost:8080/mcp
```

若 MCP 客戶端使用 stdio transport，可將啟動指令設為：

```bash
go run . mcp
```

兩種 MCP transport 都會使用網站相同的 `data/capitrack.db`。
