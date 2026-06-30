package docgen

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/tmontenegrop/kit-microsaas/config"
	"github.com/tmontenegrop/kit-microsaas/db"
	"github.com/tmontenegrop/kit-microsaas/ratelimit"
	"github.com/tmontenegrop/kit-microsaas/security"
	"github.com/tmontenegrop/kit-microsaas/template"
)

type Handler struct {
	Config *config.Config
	DB     *sql.DB
	Tmpl   *template.Engine
}

func NewHandler(cfg *config.Config, tmpl *template.Engine) *Handler {
	return &Handler{Config: cfg, DB: db.Conn, Tmpl: tmpl}
}

const (
	passPrice      = 6990
	trialMaxDocs   = 30
	trialWindowDays = 30
)

func (h *Handler) trialKey(ip string) string {
	return "trial:" + ip
}

func (h *Handler) passKey(ip string) string {
	return "pass:" + ip
}

func (h *Handler) getTrialInfo(ctx context.Context, ip string) (remaining int, passActive bool, passExpiresAt string, err error) {
	var docCount int
	var windowStart string
	err = h.DB.QueryRowContext(ctx,
		"SELECT doc_count, window_start FROM trial_tracking WHERE key = ?", h.trialKey(ip),
	).Scan(&docCount, &windowStart)
	if err == sql.ErrNoRows {
		remaining = trialMaxDocs
	} else if err != nil {
		return 0, false, "", err
	} else {
		ws, parseErr := time.Parse("2006-01-02 15:04:05", windowStart)
		if parseErr != nil || time.Since(ws) > trialWindowDays*24*time.Hour {
			remaining = trialMaxDocs
		} else {
			remaining = trialMaxDocs - docCount
			if remaining < 0 {
				remaining = 0
			}
		}
	}

	var passExpires string
	err = h.DB.QueryRowContext(ctx,
		"SELECT expires_at FROM trial_tracking WHERE key = ? AND expires_at IS NOT NULL AND expires_at > datetime('now')",
		h.passKey(ip),
	).Scan(&passExpires)
	if err == sql.ErrNoRows {
		return remaining, false, "", nil
	}
	if err != nil {
		return remaining, false, "", nil
	}
	return remaining, true, passExpires, nil
}

func (h *Handler) recordTrialUsage(ctx context.Context, ip string, docCount int) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err := h.DB.ExecContext(ctx, `
		INSERT INTO trial_tracking (key, doc_count, window_start)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			doc_count = CASE
				WHEN window_start < datetime('now', '-30 days') THEN ?
				ELSE doc_count + ?
			END,
			window_start = CASE
				WHEN window_start < datetime('now', '-30 days') THEN ?
				ELSE window_start
			END
	`, h.trialKey(ip), docCount, now, docCount, docCount, now)
	return err
}

func (h *Handler) recordPass(ctx context.Context, ip string, expiresAt time.Time) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	exp := expiresAt.Format("2006-01-02 15:04:05")
	_, err := h.DB.ExecContext(ctx, `
		INSERT INTO trial_tracking (key, doc_count, window_start, expires_at)
		VALUES (?, 0, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			expires_at = ?,
			window_start = ?
	`, h.passKey(ip), now, exp, exp, now)
	return err
}

func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	allowed, err := ratelimit.Check(r.Context(), h.DB, "upload:"+r.RemoteAddr, 10, 1*time.Hour)
	if err != nil {
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, "Demasiadas solicitudes. Intenta de nuevo mas tarde.", http.StatusTooManyRequests)
		return
	}

	if _, err := r.Cookie("device_id"); err != nil {
		http.SetCookie(w, &http.Cookie{
			Name:     "device_id",
			Value:    security.GenerateID(),
			Path:     "/",
			MaxAge:   365 * 24 * 3600,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   h.Config.IsProduction(),
		})
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "Error al leer formulario", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("template")
	if err != nil {
		http.Error(w, "Debes subir un archivo .docx", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if !strings.HasSuffix(strings.ToLower(header.Filename), ".docx") {
		http.Error(w, "Solo se aceptan archivos .docx", http.StatusBadRequest)
		return
	}

	docxBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Error al leer archivo", http.StatusInternalServerError)
		return
	}

	markers, err := extractMarkersBytes(docxBytes)
	if err != nil {
		http.Error(w, "Plantilla invalida o sin marcadores {{...}}", http.StatusBadRequest)
		return
	}

	id := security.GenerateID()
	storeDir := filepath.Join(h.Config.StoragePath, "uploads", id)
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}

	docxPath := filepath.Join(storeDir, "template.docx")
	if err := os.WriteFile(docxPath, docxBytes, 0644); err != nil {
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}

	token, _ := security.GenerateToken()
	now := time.Now().UTC()
	expiresAt := now.Add(1 * time.Hour).Format("2006-01-02 15:04:05")
	markersJSON, _ := json.Marshal(markers)

	_, err = h.DB.ExecContext(r.Context(),
		`INSERT INTO downloads (id, tool_id, token, token_hash, status, ip_address, created_at, expires_at, file_path, markers, file_name_markers)
		 VALUES (?, (SELECT id FROM tools WHERE slug = 'docgen'), ?, ?, 'draft', ?, ?, ?, ?, ?, '[]')`,
		id, token, security.HashToken(token), r.RemoteAddr, now.Format("2006-01-02 15:04:05"), expiresAt,
		docxPath, string(markersJSON),
	)
	if err != nil {
		os.RemoveAll(storeDir)
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/tools/docgen/"+id, http.StatusSeeOther)
}

func (h *Handler) Show(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var status, markersStr, fileNameMarkersStr, ipAddress string
	var dataRowsStr sql.NullString
	err := 	h.DB.QueryRowContext(r.Context(),
		"SELECT status, markers, file_name_markers, data_rows, ip_address FROM downloads WHERE id = ?", id,
	).Scan(&status, &markersStr, &fileNameMarkersStr, &dataRowsStr, &ipAddress)
	if err != nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	var markers []string
	json.Unmarshal([]byte(markersStr), &markers)

	var fileNameMarkers []string
	json.Unmarshal([]byte(fileNameMarkersStr), &fileNameMarkers)

	fileNameMarkersSet := make(map[string]bool)
	for _, m := range fileNameMarkers {
		fileNameMarkersSet[m] = true
	}

	type markerItem struct {
		Name    string
		IsFile  bool
	}

	var markerItems []markerItem
	for _, m := range markers {
		markerItems = append(markerItems, markerItem{Name: m, IsFile: fileNameMarkersSet[m]})
	}

	hasData := dataRowsStr.Valid && dataRowsStr.String != ""

	var dataRows []map[string]string
	var headers []string
	if hasData {
		json.Unmarshal([]byte(dataRowsStr.String), &dataRows)
		headers = markers
	}

	trialRemaining, passActive, passExpires, _ := h.getTrialInfo(r.Context(), ipAddress)

	var batchPrice int
	if err := h.DB.QueryRowContext(r.Context(), "SELECT price_clp FROM tools WHERE id = 'docgen'").Scan(&batchPrice); err != nil {
		batchPrice = 2990
	}

	newDocCount := 0
	if hasData {
		newDocCount = len(dataRows)
	}

	h.Tmpl.Render(w, r, "tools/docgen-show", template.TemplateData{
		Title: "Generador de Documentos",
		Data: map[string]interface{}{
			"ID":              id,
			"Status":          status,
			"Markers":         markerItems,
			"Headers":         headers,
			"DataRows":        dataRows,
			"HasData":         hasData,
			"TrialRemaining":  trialRemaining,
			"PassActive":      passActive,
			"PassExpires":     passExpires,
			"BatchPrice":      batchPrice,
			"PassPrice":       passPrice,
			"NewDocCount":     newDocCount,
			"CanUseFreeTrial": trialRemaining >= newDocCount && trialRemaining > 0,
			"IdempotencyKey":  security.GenerateID(),
		},
	})
}

func (h *Handler) DownloadTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var markersStr string
	err := 	h.DB.QueryRowContext(r.Context(),"SELECT markers FROM downloads WHERE id = ?", id).Scan(&markersStr)
	if err != nil {
		http.Error(w, "No encontrada", http.StatusNotFound)
		return
	}
	var markers []string
	json.Unmarshal([]byte(markersStr), &markers)

	f := excelize.NewFile()
	for i, m := range markers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		f.SetCellValue("Sheet1", col+"1", m)
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=plantilla.xlsx")
	f.Write(w)
}

func (h *Handler) ToggleFileNameMarker(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	marker := r.FormValue("marker")
	if marker == "" {
		http.Error(w, "marcador requerido", http.StatusBadRequest)
		return
	}

	var fileNameMarkersStr string
	err := 	h.DB.QueryRowContext(r.Context(),"SELECT file_name_markers FROM downloads WHERE id = ?", id).Scan(&fileNameMarkersStr)
	if err != nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	var markers []string
	json.Unmarshal([]byte(fileNameMarkersStr), &markers)

	found := false
	for i, m := range markers {
		if m == marker {
			markers = append(markers[:i], markers[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		markers = append(markers, marker)
	}

	updated, _ := json.Marshal(markers)
	h.DB.ExecContext(r.Context(),"UPDATE downloads SET file_name_markers = ? WHERE id = ?", string(updated), id)

	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) DataUpload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if allowed, err := ratelimit.Check(r.Context(), h.DB, "data:"+r.RemoteAddr, 20, 1*time.Hour); err != nil {
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	} else if !allowed {
		http.Error(w, "Demasiadas solicitudes. Intenta de nuevo mas tarde.", http.StatusTooManyRequests)
		return
	}

	var status, markersStr string
	err := 	h.DB.QueryRowContext(r.Context(),"SELECT status, markers FROM downloads WHERE id = ?", id).Scan(&status, &markersStr)
	if err != nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}

	var markers []string
	json.Unmarshal([]byte(markersStr), &markers)

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "Error al leer formulario", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("excel")
	if err != nil {
		http.Error(w, "Debes subir un archivo Excel", http.StatusBadRequest)
		return
	}
	defer file.Close()

	excelBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Error al leer archivo", http.StatusInternalServerError)
		return
	}

	xlsx, err := excelize.OpenReader(bytes.NewReader(excelBytes))
	if err != nil {
		http.Error(w, "Excel invalido", http.StatusBadRequest)
		return
	}

	sheets := xlsx.GetSheetList()
	if len(sheets) == 0 {
		http.Error(w, "El Excel no contiene hojas", http.StatusBadRequest)
		return
	}
	sheetName := sheets[0]
	rows, err := xlsx.GetRows(sheetName)
	if err != nil || len(rows) < 2 {
		http.Error(w, "El Excel debe tener encabezados + al menos 1 fila de datos", http.StatusBadRequest)
		return
	}

	headers := rows[0]
	colMap := make(map[string]int)
	for i, h := range headers {
		colMap[h] = i
	}

	var missing []string
	for _, m := range markers {
		if _, ok := colMap[m]; !ok {
			missing = append(missing, m)
		}
	}
	if len(missing) > 0 {
		http.Error(w, "Faltan columnas en el Excel: "+strings.Join(missing, ", "), http.StatusBadRequest)
		return
	}

	var dataRows []map[string]string
	for _, rec := range rows[1:] {
		row := make(map[string]string)
		for _, m := range markers {
			if idx, ok := colMap[m]; ok && idx < len(rec) {
				row[m] = rec[idx]
			}
		}
		dataRows = append(dataRows, row)
	}

	if len(dataRows) > 300 {
		http.Error(w, "Maximo 300 filas por archivo", http.StatusBadRequest)
		return
	}

	dataRowsJSON, _ := json.Marshal(dataRows)
	if _, err := h.DB.ExecContext(r.Context(),"UPDATE downloads SET data_rows = ?, status = 'ready' WHERE id = ?", string(dataRowsJSON), id); err != nil {
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/tools/docgen/"+id, http.StatusSeeOther)
}

func (h *Handler) Pay(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	plan := r.FormValue("plan")

	if allowed, err := ratelimit.Check(r.Context(), h.DB, "pay:"+r.RemoteAddr, 10, 1*time.Hour); err != nil {
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	} else if !allowed {
		http.Error(w, "Demasiadas solicitudes. Intenta de nuevo mas tarde.", http.StatusTooManyRequests)
		return
	}

	// Idempotency check
	idk := r.FormValue("idempotency_key")
	if idk != "" {
		var exists int
		err := h.DB.QueryRowContext(r.Context(),
			"SELECT 1 FROM idempotency_keys WHERE key = ? AND expires_at > datetime('now')", "pay:"+idk,
		).Scan(&exists)
		if err == nil {
			http.Error(w, "Solicitud duplicada", http.StatusConflict)
			return
		}
		h.DB.ExecContext(r.Context(),
			"INSERT INTO idempotency_keys (key, expires_at) VALUES (?, datetime('now', '+1 hour')) ON CONFLICT(key) DO NOTHING",
			"pay:"+idk,
		)
	}

	var status, token, ipAddress string
	var dataRowsStr sql.NullString
	err := h.DB.QueryRowContext(r.Context(),
		"SELECT status, token, ip_address, data_rows FROM downloads WHERE id = ?", id,
	).Scan(&status, &token, &ipAddress, &dataRowsStr)
	if err != nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}
	if status != "ready" || !dataRowsStr.Valid {
		http.Error(w, "Debes subir los datos antes de continuar", http.StatusBadRequest)
		return
	}

	var dataRows []map[string]string
	json.Unmarshal([]byte(dataRowsStr.String), &dataRows)
	docCount := len(dataRows)

	switch plan {
	case "free":
		remaining, _, _, err := h.getTrialInfo(r.Context(), ipAddress)
		if err != nil || remaining < docCount {
			http.Error(w, "Has agotado tu prueba gratuita. Elige un plan de pago.", http.StatusPaymentRequired)
			return
		}
		if err := h.recordTrialUsage(r.Context(), ipAddress, docCount); err != nil {
			http.Error(w, "Error interno", http.StatusInternalServerError)
			return
		}
			if err := h.generateAndServe(r.Context(), id, token, 0, ""); err != nil {
			http.Error(w, "Error interno", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/download/"+token, http.StatusSeeOther)

	case "pass":
		if h.Config.IsDevelopment() {
			if err := h.recordPass(r.Context(), ipAddress, time.Now().UTC().Add(30*24*time.Hour)); err != nil {
				http.Error(w, "Error interno", http.StatusInternalServerError)
				return
			}
			if err := h.generateAndServe(r.Context(), id, token, passPrice, ""); err != nil {
				http.Error(w, "Error interno", http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, "/download/"+token, http.StatusSeeOther)
			return
		}
		flowURL, err := h.createFlowPayment(token, passPrice, "Pase 30 dias - DocGen")
		if err != nil {
			http.Error(w, "Error al iniciar pago", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, flowURL, http.StatusSeeOther)

	default:
		http.Error(w, "Plan invalido", http.StatusBadRequest)
		return

	case "batch":
		var batchPrice int
		err := h.DB.QueryRowContext(r.Context(), "SELECT price_clp FROM tools WHERE id = 'docgen'").Scan(&batchPrice)
		if err != nil {
			batchPrice = 2990
		}
		if h.Config.IsDevelopment() {
			if err := h.generateAndServe(r.Context(), id, token, batchPrice, ""); err != nil {
				http.Error(w, "Error interno", http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, "/download/"+token, http.StatusSeeOther)
			return
		}
		flowURL, err := h.createFlowPayment(token, batchPrice, "Batch - Generacion de Documentos")
		if err != nil {
			http.Error(w, "Error al iniciar pago", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, flowURL, http.StatusSeeOther)
	}
}

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	var status, id string
	err := h.DB.QueryRowContext(r.Context(), "SELECT id, status FROM downloads WHERE token_hash = ?", security.HashToken(token)).Scan(&id, &status)
	if err != nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}
	h.Tmpl.Render(w, r, "tools/status", template.TemplateData{
		Title: "Estado del pago",
		Data:  map[string]interface{}{"Status": status, "ID": id, "Token": token},
	})
}

func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	tokenHash := security.HashToken(token)

	var id, filePath, status string
	err := 	h.DB.QueryRowContext(r.Context(),"SELECT id, file_path, status FROM downloads WHERE token_hash = ?", tokenHash).Scan(&id, &filePath, &status)
	if err != nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}
	if status != "paid" {
		http.Error(w, "Pago no confirmado", http.StatusPaymentRequired)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=documentos.zip")
	http.ServeFile(w, r, filePath)
}

func (h *Handler) Webhook(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "error", http.StatusBadRequest)
		return
	}

	token := r.FormValue("token")
	signature := r.FormValue("signature")
	body := r.FormValue("body")

	if !security.VerifyHMAC(signature, body, h.Config.FlowSecretKey) {
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}

	var data struct {
		Token  string `json:"token"`
		Status string `json:"status"`
		Amount int    `json:"amount"`
	}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if data.Status != "success" {
		w.WriteHeader(http.StatusOK)
		return
	}

	tokenHash := security.HashToken(token)

	var downloadID, ipAddress string
	err := h.DB.QueryRowContext(r.Context(),
		"SELECT id, ip_address FROM downloads WHERE token_hash = ? AND status = 'ready'", tokenHash,
	).Scan(&downloadID, &ipAddress)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	amount := data.Amount
	if amount <= 0 {
		amount = 2990
	}

	if err := h.generateAndServe(r.Context(), downloadID, token, amount, data.Token); err != nil {
		slog.Error("webhook generateAndServe", "download_id", downloadID, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if amount >= passPrice {
		if err := h.recordPass(r.Context(), ipAddress, time.Now().UTC().Add(30*24*time.Hour)); err != nil {
			slog.Error("webhook recordPass", "ip", ipAddress, "error", err)
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) generateAndServe(ctx context.Context, id, token string, amount int, flowToken string) error {
	var docxPath, markersStr, fileNameMarkersStr string
	var dataRowsStr sql.NullString
	err := h.DB.QueryRowContext(ctx,
		"SELECT file_path, markers, file_name_markers, data_rows FROM downloads WHERE id = ?", id,
	).Scan(&docxPath, &markersStr, &fileNameMarkersStr, &dataRowsStr)
	if err != nil {
		return fmt.Errorf("query download: %w", err)
	}
	if !dataRowsStr.Valid {
		return fmt.Errorf("no data rows")
	}

	var markers []string
	json.Unmarshal([]byte(markersStr), &markers)
	var fileNameMarkers []string
	json.Unmarshal([]byte(fileNameMarkersStr), &fileNameMarkers)
	var dataRows []map[string]string
	json.Unmarshal([]byte(dataRowsStr.String), &dataRows)

	templateBytes, err := os.ReadFile(docxPath)
	if err != nil {
		return fmt.Errorf("read template: %w", err)
	}

	storeDir := filepath.Join(h.Config.StoragePath, "downloads", id)
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	zipPath := filepath.Join(storeDir, "documentos.zip")
	if err := generateZip(templateBytes, markers, fileNameMarkers, dataRows, zipPath); err != nil {
		return fmt.Errorf("generate zip: %w", err)
	}

	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "UPDATE downloads SET status = 'paid', paid_at = datetime('now'), file_path = ? WHERE id = ?", zipPath, id); err != nil {
		return fmt.Errorf("update download: %w", err)
	}
	paymentID := security.GenerateID()
	if _, err := tx.ExecContext(ctx, "INSERT INTO payments (id, download_id, amount, status, flow_token) VALUES (?, ?, ?, 'confirmed', ?)", paymentID, id, amount, flowToken); err != nil {
		return fmt.Errorf("insert payment: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (h *Handler) createFlowPayment(token string, amount int, subject string) (string, error) {
	parts := strings.SplitN(h.Config.FlowAPIKey, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid api key format")
	}
	apiKey := parts[0]
	commerceID := parts[1]

	vals := url.Values{
		"apiKey":          {apiKey},
		"commerceId":      {commerceID},
		"subject":         {subject},
		"currency":        {"CLP"},
		"amount":          {fmt.Sprintf("%d", amount)},
		"email":           {""},
		"urlConfirmation": {h.Config.AppURL + "/webhook/flow"},
		"urlReturn":       {h.Config.AppURL + "/status/" + token},
	}

	resp, err := http.PostForm(h.Config.FlowBaseURL+"/api/payment/create", vals)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		URL   string `json:"url"`
		Token string `json:"token"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Error != "" {
		return "", fmt.Errorf("flow error: %s", result.Error)
	}

	return result.URL, nil
}

func generateZip(templateBytes []byte, markers []string, fileNameMarkers []string, rows []map[string]string, outputPath string) error {
	zipFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	for i, row := range rows {
		doc := replaceMarkers(templateBytes, markers, func(m string) string { return row[m] })

		docName := fmt.Sprintf("documento-%d.docx", i+1)
		if len(fileNameMarkers) > 0 {
			var parts []string
			for _, m := range fileNameMarkers {
				if v, ok := row[m]; ok && v != "" {
					parts = append(parts, v)
				}
			}
			if len(parts) > 0 {
				name := sanitizeFilename(strings.Join(parts, " - "))
				if name != "" {
					docName = name + ".docx"
				}
			}
		}

		f, _ := zipWriter.Create(docName)
		f.Write(doc)
	}
	return nil
}

func sanitizeFilename(s string) string {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer(
		"\\", "", "/", "", ":", "", "*", "", "?", "", "\"", "", "<", "", ">", "", "|", "",
	).Replace(s)
	if s == "" {
		return "sin-nombre"
	}
	return s
}

var (
	markerRegex = regexp.MustCompile(`\{\{([\pL0-9_]+)\}\}`)
	wtRegex     = regexp.MustCompile(`<w:t[^>]*>([^<]*)</w:t>`)
	wpRegex     = regexp.MustCompile(`(?s)<w:p\b[^>]*>.*?</w:p>`)
	wrRegex     = regexp.MustCompile(`(?s)<w:r\b[^>]*>.*?</w:r>`)
	wtxRegex    = regexp.MustCompile(`<w:t[^>]*>.*?</w:t>`)
)

func extractMarkersBytes(docxBytes []byte) ([]string, error) {
	r, err := zip.NewReader(bytes.NewReader(docxBytes), int64(len(docxBytes)))
	if err != nil {
		return nil, fmt.Errorf("no es un .docx valido: %w", err)
	}

	var docXML bytes.Buffer
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			_, _ = io.Copy(&docXML, rc)
			rc.Close()
			break
		}
	}
	if docXML.Len() == 0 {
		return nil, fmt.Errorf("no se encontro word/document.xml")
	}

	var allText string
	for _, m := range wtRegex.FindAllStringSubmatch(docXML.String(), -1) {
		allText += m[1]
	}

	seen := make(map[string]struct{})
	var markers []string
	for _, m := range markerRegex.FindAllStringSubmatch(allText, -1) {
		name := m[1]
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			markers = append(markers, name)
		}
	}
	if len(markers) == 0 {
		return nil, fmt.Errorf("no se encontraron marcadores {{...}} en la plantilla")
	}
	return markers, nil
}

func replaceMarkers(templateBytes []byte, markers []string, lookup func(string) string) []byte {
	r, err := zip.NewReader(bytes.NewReader(templateBytes), int64(len(templateBytes)))
	if err != nil {
		return templateBytes
	}

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			continue
		}

		if f.Name == "word/document.xml" {
			var content bytes.Buffer
			_, _ = io.Copy(&content, rc)
			rc.Close()

			xml := content.String()
			xml = replaceInDocXML(xml, markers, lookup)

			zh, _ := w.Create(f.Name)
			zh.Write([]byte(xml))
		} else {
			zh, _ := w.Create(f.Name)
			_, _ = io.Copy(zh, rc)
			rc.Close()
		}
	}

	w.Close()
	return buf.Bytes()
}

func replaceInDocXML(xml string, markers []string, lookup func(string) string) string {
	return wpRegex.ReplaceAllStringFunc(xml, func(p string) string {
		runs := wrRegex.FindAllString(p, -1)
		if len(runs) == 0 {
			return p
		}

		var parts []string
		for _, r := range runs {
			tm := wtRegex.FindStringSubmatch(r)
			if len(tm) > 1 {
				parts = append(parts, tm[1])
			} else {
				parts = append(parts, "")
			}
		}
		joined := strings.Join(parts, "")

		hasMarker := false
		for _, m := range markers {
			if strings.Contains(joined, "{{"+m+"}}") {
				hasMarker = true
				break
			}
		}
		if !hasMarker {
			return p
		}

		for _, m := range markers {
			joined = strings.ReplaceAll(joined, "{{"+m+"}}", escapeXML(lookup(m)))
		}

		result := p
		for i, r := range runs {
			newText := ""
			if i == 0 {
				newText = joined
			}
			newRun := wtxRegex.ReplaceAllString(r, "<w:t xml:space=\"preserve\">"+newText+"</w:t>")
			result = strings.Replace(result, r, newRun, 1)
		}
		return result
	})
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
