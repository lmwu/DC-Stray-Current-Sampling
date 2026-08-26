package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
)

// ResponseData 定義傳給網頁前端的 JSON 結構
type ResponseData struct {
	Station   string  `json:"station"`
	Potential float64 `json:"potential"`
	Current   float64 `json:"current"`
	Timestamp string  `json:"timestamp"`
	Error     string  `json:"error,omitempty"`
}

var opcClient *opcua.Client

func main() {
	endpoint := "opc.tcp://localhost:4840"
	ctx := context.Background()

	// 1. 初始化 Go OPC UA Client (SecurityPolicy: None)
	fmt.Printf("正在連接 OPC UA Server (%s)...\n", endpoint)
	c, err := opcua.NewClient(endpoint, opcua.SecurityMode(ua.MessageSecurityModeNone))
	if err != nil {
		log.Fatalf("建立 Client 失敗: %v", err)
	}

	if err := c.Connect(ctx); err != nil {
		log.Fatalf("連接 Server 失敗: %v", err)
	}
	defer c.Close(ctx)
	opcClient = c

	fmt.Println("成功連接至 Julia OPC UA 伺服器！")

	// 2. 設定 Web API 路由
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/data", handleAPIData)

	fmt.Println("==================================================")
	fmt.Println("  🌐 Web 控制台已啟動: http://localhost:8080")
	fmt.Println("==================================================")

	// 啟動 Web 服務
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// 讀取指定 Station 的 DC 電位與電流
func readStationData(ctx context.Context, stationID string) (*ResponseData, error) {
	potNodeIDStr := fmt.Sprintf("ns=1;s=%s.DCPotential", stationID)
	curNodeIDStr := fmt.Sprintf("ns=1;s=%s.DCCurrent", stationID)

	potNodeID, err := ua.ParseNodeID(potNodeIDStr)
	if err != nil {
		return nil, fmt.Errorf("解析電位 NodeID 失敗: %v", err)
	}
	curNodeID, err := ua.ParseNodeID(curNodeIDStr)
	if err != nil {
		return nil, fmt.Errorf("解析電流 NodeID 失敗: %v", err)
	}

	// 批次即時讀取 OPC UA 節點 (MaxAge: 0 強制讀取 Server 記憶體最新值)
	req := &ua.ReadRequest{
		MaxAge: 0,
		NodesToRead: []*ua.ReadValueID{
			{NodeID: potNodeID},
			{NodeID: curNodeID},
		},
		TimestampsToReturn: ua.TimestampsToReturnBoth,
	}

	res, err := opcClient.Read(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("讀取數據失敗: %v", err)
	}

	if len(res.Results) < 2 {
		return nil, fmt.Errorf("回傳結果數量不齊全")
	}

	potVal := res.Results[0].Value.Float()
	curVal := res.Results[1].Value.Float()

	return &ResponseData{
		Station:   stationID,
		Potential: potVal,
		Current:   curVal,
		Timestamp: time.Now().Format("15:04:05"),
	}, nil
}

// API 處理常式: /api/data?station=Station_001
func handleAPIData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	stationID := r.URL.Query().Get("station")
	if stationID == "" {
		stationID = "Station_001"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	data, err := readStationData(ctx, stationID)
	if err != nil {
		json.NewEncoder(w).Encode(ResponseData{Error: err.Error()})
		return
	}

	json.NewEncoder(w).Encode(data)
}

// 首頁網頁 HTML 模板 (已修正 JS 引號衝突)
func handleIndex(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html lang="zh-TW">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>腐蝕防蝕 CP 模擬器監控儀表板</title>
    <style>
        body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background-color: #0f172a; color: #f8fafc; margin: 0; padding: 30px; }
        .container { max-width: 800px; margin: 0 auto; }
        h1 { text-align: center; color: #38bdf8; font-weight: 600; margin-bottom: 30px; }
        .controls { display: flex; justify-content: center; align-items: center; gap: 15px; margin-bottom: 30px; }
        label { font-size: 1.1rem; color: #94a3b8; }
        select { background-color: #1e293b; color: #38bdf8; border: 2px solid #3b82f6; border-radius: 8px; padding: 10px 20px; font-size: 1.1rem; outline: none; cursor: pointer; }
        .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 20px; }
        .card { background-color: #1e293b; border-radius: 12px; padding: 25px; border: 1px solid #334155; text-align: center; box-shadow: 0 10px 15px -3px rgba(0, 0, 0, 0.3); transition: transform 0.2s; }
        .card:hover { transform: translateY(-3px); }
        .card h3 { margin: 0 0 15px 0; font-size: 1.2rem; color: #94a3b8; }
        .value { font-size: 2.8rem; font-weight: bold; margin: 10px 0; }
        .pot-val { color: #4ade80; }
        .cur-val { color: #facc15; }
        .unit { font-size: 1.2rem; color: #64748b; margin-left: 5px; }
        .footer { text-align: center; margin-top: 30px; color: #64748b; font-size: 0.9rem; }
        .badge { display: inline-block; padding: 4px 10px; background-color: #0284c7; color: white; border-radius: 20px; font-size: 0.8rem; margin-top: 10px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>⚡ 腐蝕防蝕 DC 監控儀表板</h1>
        <div class="controls">
            <label for="stationSelect">選擇測試測站：</label>
            <select id="stationSelect" onchange="fetchData()"></select>
        </div>

        <div class="grid">
            <div class="card">
                <h3>DC 電位 (DCPotential)</h3>
                <div class="value pot-val"><span id="potValue">--</span><span class="unit">V</span></div>
                <div class="badge">陰極保護指標</div>
            </div>
            <div class="card">
                <h3>DC 電流 (DCCurrent)</h3>
                <div class="value cur-val"><span id="curValue">--</span><span class="unit">mA</span></div>
                <div class="badge">輸出電流</div>
            </div>
        </div>

        <div class="footer">
            <p>最後更新時間：<span id="lastUpdated">--:--:--</span> (每秒自動刷新)</p>
            <p style="color: #475569;">後端架構: Go Client + Julia OPC UA Server (open62541)</p>
        </div>
    </div>

    <script>
        // 初始化 100 個 Station 下拉選單
        const select = document.getElementById('stationSelect');
        for (let i = 1; i <= 100; i++) {
            const num = String(i).padStart(3, '0');
            const opt = document.createElement('option');
            opt.value = 'Station_' + num;
            opt.textContent = '測站 Station_' + num;
            select.appendChild(opt);
        }

        // 向 Go 後端 API 抓取數據 (加上時間戳記防止快取)
        async function fetchData() {
            const station = select.value;
            try {
                const response = await fetch('/api/data?station=' + station + '&_t=' + Date.now());
                const data = await response.json();

                if (data.error) {
                    console.error("API 錯誤:", data.error);
                    return;
                }

                document.getElementById('potValue').textContent = data.potential.toFixed(3);
                document.getElementById('curValue').textContent = data.current.toFixed(2);
                document.getElementById('lastUpdated').textContent = data.timestamp;
            } catch (err) {
                console.error("請求失敗:", err);
            }
        }

        // 頁面載入時先抓一次，隨後每 1 秒自動刷新一次
        fetchData();
        setInterval(fetchData, 1000);
    </script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}
