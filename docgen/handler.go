package docgen

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
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

func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "Error al leer plantilla: "+err.Error(), http.StatusBadRequest)
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

	_, err = h.DB.Exec(
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

	var status, markersStr, fileNameMarkersStr string
	var dataRowsStr sql.NullString
	err := h.DB.QueryRow(
		"SELECT status, markers, file_name_markers, data_rows FROM downloads WHERE id = ?", id,
	).Scan(&status, &markersStr, &fileNameMarkersStr, &dataRowsStr)
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

	h.Tmpl.Render(w, r, "tools/docgen-show", template.TemplateData{
		Title: "Generador de Documentos",
		Data: map[string]interface{}{
			"ID":             id,
			"Status":         status,
			"Markers":        markerItems,
			"Headers":        headers,
			"DataRows":       dataRows,
			"HasData":        hasData,
			"IdempotencyKey": security.GenerateID(),
		},
	})
}

func (h *Handler) DownloadTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var markersStr string
	err := h.DB.QueryRow("SELECT markers FROM downloads WHERE id = ?", id).Scan(&markersStr)
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
	err := h.DB.QueryRow("SELECT file_name_markers FROM downloads WHERE id = ?", id).Scan(&fileNameMarkersStr)
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
	h.DB.Exec("UPDATE downloads SET file_name_markers = ? WHERE id = ?", string(updated), id)

	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) DataUpload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var status, markersStr string
	err := h.DB.QueryRow("SELECT status, markers FROM downloads WHERE id = ?", id).Scan(&status, &markersStr)
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

	rows, err := xlsx.GetRows("Sheet1")
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

	dataRowsJSON, _ := json.Marshal(dataRows)
	h.DB.Exec("UPDATE downloads SET data_rows = ?, status = 'ready' WHERE id = ?", string(dataRowsJSON), id)

	http.Redirect(w, r, "/tools/docgen/"+id, http.StatusSeeOther)
}

func (h *Handler) Pay(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var status, token string
	err := h.DB.QueryRow("SELECT status, token FROM downloads WHERE id = ?", id).Scan(&status, &token)
	if err != nil {
		http.Error(w, "No encontrado", http.StatusNotFound)
		return
	}
	if status != "ready" {
		http.Error(w, "Debes subir los datos antes de pagar", http.StatusBadRequest)
		return
	}

	if h.Config.IsDevelopment() {
		h.generateAndServe(id, token)
		http.Redirect(w, r, "/download/"+token, http.StatusSeeOther)
		return
	}

	flowURL, err := h.createFlowPayment(token)
	if err != nil {
		http.Error(w, "Error al iniciar pago", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, flowURL, http.StatusSeeOther)
}

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	var status, id string
	err := h.DB.QueryRow("SELECT id, status FROM downloads WHERE token_hash = ?", security.HashToken(token)).Scan(&id, &status)
	if err != nil {
		h.Tmpl.Render(w, r, "tools/docgen", template.TemplateData{Title: "No encontrado"})
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
	err := h.DB.QueryRow("SELECT id, file_path, status FROM downloads WHERE token_hash = ?", tokenHash).Scan(&id, &filePath, &status)
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

	var downloadID string
	err := h.DB.QueryRow(
		"SELECT id FROM downloads WHERE token_hash = ? AND status = 'ready'", tokenHash,
	).Scan(&downloadID)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	h.generateAndServe(downloadID, token)

	var price int
	h.DB.QueryRow("SELECT price_clp FROM tools t JOIN downloads d ON d.tool_id = t.id WHERE d.id = ?", downloadID).Scan(&price)

	// Insert payment with flow_token
	h.DB.Exec("UPDATE payments SET flow_token = ? WHERE download_id = ? AND flow_token IS NULL", data.Token, downloadID)
	// If no payment row exists, insert one
	var count int
	h.DB.QueryRow("SELECT COUNT(*) FROM payments WHERE download_id = ?", downloadID).Scan(&count)
	if count == 0 {
		h.DB.Exec("INSERT INTO payments (id, download_id, amount, status, flow_token) VALUES (?, ?, ?, 'confirmed', ?)",
			security.GenerateID(), downloadID, price, data.Token)
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) generateAndServe(id, token string) {
	var docxPath, markersStr, fileNameMarkersStr string
	var dataRowsStr sql.NullString
	err := h.DB.QueryRow(
		"SELECT file_path, markers, file_name_markers, data_rows FROM downloads WHERE id = ?", id,
	).Scan(&docxPath, &markersStr, &fileNameMarkersStr, &dataRowsStr)
	if err != nil || !dataRowsStr.Valid {
		return
	}

	var markers []string
	json.Unmarshal([]byte(markersStr), &markers)
	var fileNameMarkers []string
	json.Unmarshal([]byte(fileNameMarkersStr), &fileNameMarkers)
	var dataRows []map[string]string
	json.Unmarshal([]byte(dataRowsStr.String), &dataRows)

	templateBytes, err := os.ReadFile(docxPath)
	if err != nil {
		return
	}

	storeDir := filepath.Join(h.Config.StoragePath, "downloads", id)
	os.MkdirAll(storeDir, 0755)
	zipPath := filepath.Join(storeDir, "documentos.zip")
	if err := generateZip(templateBytes, markers, fileNameMarkers, dataRows, zipPath); err != nil {
		return
	}

	tokenHash := security.HashToken(token)
	h.DB.Exec("UPDATE downloads SET status = 'paid', paid_at = datetime('now'), file_path = ? WHERE id = ?", zipPath, id)
	h.DB.Exec("INSERT INTO payments (id, download_id, amount, status) VALUES (?, ?, 2990, 'confirmed')", security.GenerateID(), id)
	_ = tokenHash
}

func (h *Handler) createFlowPayment(token string) (string, error) {
	parts := strings.SplitN(h.Config.FlowAPIKey, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid api key format")
	}
	apiKey := parts[0]
	commerceID := parts[1]

	vals := url.Values{
		"apiKey":          {apiKey},
		"commerceId":      {commerceID},
		"subject":         {"DocGen - Generacion de Documentos"},
		"currency":        {"CLP"},
		"amount":          {"2990"},
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
