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

## MCP

CapiTrack 使用官方 Go SDK，提供四個 MCP tools：

- `get_portfolio`：讀取目前持股、總值、成本與損益
- `list_transactions`：依標的或日期區間查詢交易
- `add_transaction`：新增買入或賣出交易
- `update_current_price`：更新標的目前市價

網站啟動後，Streamable HTTP MCP endpoint 位於：

```text
http://localhost:8080/mcp
```

若 MCP 客戶端使用 stdio transport，可將啟動指令設為：

```bash
go run . mcp
```

兩種 MCP transport 都會使用網站相同的 `data/capitrack.db`。
