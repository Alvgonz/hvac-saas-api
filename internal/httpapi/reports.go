package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/phpdave11/gofpdf"
)

type ReportsHandler struct {
	DB *pgxpool.Pool
}

type reportRow struct {
	CompletedAt time.Time
	WorkOrderID string
	SiteName    string
	AssetTag    string
	AssetName   string
	Type        string
	Priority    string
	Title       string
}

func (h *ReportsHandler) Monthly(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	monthStr := strings.TrimSpace(r.URL.Query().Get("month"))
	if monthStr == "" {
		http.Error(w, "month is required (YYYY-MM)", http.StatusBadRequest)
		return
	}

	monthStart, err := time.Parse("2006-01", monthStr)
	if err != nil {
		http.Error(w, "invalid month format, expected YYYY-MM", http.StatusBadRequest)
		return
	}
	// Ventana [start, end)
	start := monthStart
	end := monthStart.AddDate(0, 1, 0)

	// customer_id:
	// - client: solo el suyo (del token)
	// - admin/dispatcher: puede mandar customer_id por query
	// - technician: por ahora no (lo puedes habilitar luego si quieres)
	var customerID string
	if claims.Role == "client" {
		if claims.CustomerID == nil {
			http.Error(w, "client has no customer_id", http.StatusForbidden)
			return
		}
		customerID = *claims.CustomerID
	} else if claims.Role == "admin" || claims.Role == "dispatcher" {
		customerID = strings.TrimSpace(r.URL.Query().Get("customer_id"))
		if customerID == "" {
			http.Error(w, "customer_id is required for admin/dispatcher", http.StatusBadRequest)
			return
		}
	} else {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	// Trae encabezados (provider y customer)
	var providerName, customerName string
	err = h.DB.QueryRow(ctx, `
		SELECT sp.name, c.name
		FROM service_provider sp
		JOIN customer c ON c.service_provider_id = sp.id
		WHERE sp.id = $1 AND c.id = $2
	`, claims.ServiceProvider, customerID).Scan(&providerName, &customerName)
	if err != nil {
		http.Error(w, "provider/customer not found", http.StatusNotFound)
		return
	}

	// Work orders completadas en el mes
	rows, err := h.DB.Query(ctx, `
		SELECT
			wo.completed_at,
			wo.id,
			s.name AS site_name,
			a.tag_code,
			COALESCE(a.name, '') AS asset_name,
			wo.type,
			wo.priority,
			wo.title
		FROM work_order wo
		JOIN site s ON s.id = wo.site_id
		JOIN asset a ON a.id = wo.asset_id
		WHERE wo.service_provider_id = $1
		  AND wo.customer_id = $2
		  AND wo.status = 'completed'
		  AND wo.completed_at >= $3
		  AND wo.completed_at <  $4
		ORDER BY wo.completed_at ASC
	`, claims.ServiceProvider, customerID, start, end)
	if err != nil {
		http.Error(w, "query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := make([]reportRow, 0, 100)
	for rows.Next() {
		var it reportRow
		if err := rows.Scan(
			&it.CompletedAt,
			&it.WorkOrderID,
			&it.SiteName,
			&it.AssetTag,
			&it.AssetName,
			&it.Type,
			&it.Priority,
			&it.Title,
		); err != nil {
			http.Error(w, "scan error", http.StatusInternalServerError)
			return
		}
		items = append(items, it)
	}

	// Generar PDF
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetTitle("Monthly Maintenance Report", false)

	// Fuente UTF-8 (TTF)
	pdf.AddUTF8Font("Body", "", "internal/assets/fonts/AndaleMono.ttf")
	pdf.AddUTF8Font("Body", "B", "internal/assets/fonts/AndaleMono.ttf")


	pdf.AddPage()

	pdf.SetFont("Body", "B", 16)
	pdf.Cell(0, 10, "Monthly Maintenance Report")
	pdf.Ln(8)

	pdf.SetFont("Body", "", 11)
	pdf.Cell(0, 7, fmt.Sprintf("Service Provider: %s", providerName))
	pdf.Ln(6)
	pdf.Cell(0, 7, fmt.Sprintf("Customer: %s", customerName))
	pdf.Ln(6)
	pdf.Cell(0, 7, fmt.Sprintf("Period: %s", monthStr))
	pdf.Ln(8)

	pdf.SetFont("Body", "B", 11)
	pdf.Cell(0, 7, fmt.Sprintf("Completed Work Orders: %d", len(items)))
	pdf.Ln(10)


	// Tabla
	pdf.SetFont("Body", "B", 9)
	colW := []float64{22, 28, 30, 40, 70} // date, type, priority, asset, title
	headers := []string{"Date", "Type", "Priority", "Asset", "Title"}

	for i, h := range headers {
		pdf.CellFormat(colW[i], 7, h, "1", 0, "LM", false, 0, "")
	}
	pdf.Ln(-1)

	pdf.SetFont("Body", "", 9)
	lineH := 6.0
	padX := 1.5
	padTop := 2.0
	padBottom := 2.0

	for _, it := range items {
		x0 := pdf.GetX()
		y := pdf.GetY()

		dateStr := it.CompletedAt.Format("2006-01-02")

		assetStr := it.AssetTag
		if it.AssetName != "" {
			assetStr = fmt.Sprintf("%s - %s", it.AssetTag, it.AssetName)
		}

		// wrap a máximo 2 líneas
		assetLines := wrap2(pdf, assetStr, colW[3]-2)
		titleLines := wrap2(pdf, it.Title, colW[4]-2)

		// altura de fila según el que tenga más líneas
		maxLines := len(assetLines)
		if len(titleLines) > maxLines {
			maxLines = len(titleLines)
		}
		if maxLines < 1 {
			maxLines = 1
		}
		rowH := padTop + float64(maxLines)*lineH + padBottom

		x := x0

		// Date
		pdf.Rect(x, y, colW[0], rowH, "")
		pdf.Text(x+padX, y+padTop+lineH, dateStr)
		x += colW[0]

		// Type
		pdf.Rect(x, y, colW[1], rowH, "")
		pdf.Text(x+padX, y+padTop+lineH, it.Type)
		x += colW[1]

		// Priority
		pdf.Rect(x, y, colW[2], rowH, "")
		pdf.Text(x+padX, y+padTop+lineH, it.Priority)
		x += colW[2]

		// Asset (wrap)
		pdf.Rect(x, y, colW[3], rowH, "")
		for i := 0; i < len(assetLines); i++ {
			pdf.Text(x+padX, y+padTop+lineH*float64(i+1), assetLines[i])
		}
		x += colW[3]

		// Title (wrap)
		pdf.Rect(x, y, colW[4], rowH, "")
		for i := 0; i < len(titleLines); i++ {
			pdf.Text(x+padX, y+padTop+lineH*float64(i+1), titleLines[i])
		}

		// siguiente fila: vuelve al inicio (x0)
		pdf.SetXY(x0, y+rowH)
	}

	// Respuesta HTTP
	filename := fmt.Sprintf("maintenance_%s_%s.pdf", sanitizeFilename(customerName), monthStr)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	_ = pdf.Output(w)
}


func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func sanitizeFilename(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	return s
}

func truncateToWidth(pdf *gofpdf.Fpdf, s string, w float64) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	// padding interno para que no pegue al borde
	maxW := w - 2
	if maxW <= 0 {
		return ""
	}

	// ya cabe
	if pdf.GetStringWidth(s) <= maxW {
		return s
	}

	ellipsis := "..."
	r := []rune(s)
	for len(r) > 0 {
		candidate := string(r) + ellipsis
		if pdf.GetStringWidth(candidate) <= maxW {
			return candidate
		}
		r = r[:len(r)-1]
	}
	return ellipsis
}

func wrap2(pdf *gofpdf.Fpdf, s string, w float64) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{""}
	}

	lines := pdf.SplitLines([]byte(s), w)
	if len(lines) == 0 {
		return []string{""}
	}

	out := make([]string, 0, 2)

	// línea 1
	out = append(out, string(lines[0]))

	// línea 2 (si existe)
	if len(lines) > 1 {
		second := string(lines[1])

		// si había más líneas, truncamos la 2da con "..."
		if len(lines) > 2 {
			second = fitWithEllipsis(pdf, second, w)
		}
		out = append(out, second)
	}

	// si solo cabía en 1 línea, listo
	return out
}

func fitWithEllipsis(pdf *gofpdf.Fpdf, s string, w float64) string {
	ellipsis := "..."
	if pdf.GetStringWidth(s) <= w {
		return s
	}
	r := []rune(strings.TrimSpace(s))
	for len(r) > 0 {
		candidate := string(r) + ellipsis
		if pdf.GetStringWidth(candidate) <= w {
			return candidate
		}
		r = r[:len(r)-1]
	}
	return ellipsis
}
