package pairing

import (
	"encoding/base64"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"msm-client/config"

	qrcode "github.com/skip2/go-qrcode"
)

// PairingDisplay handles the display of pairing codes and QR codes
type PairingDisplay struct {
	templatePath   string
	template       *template.Template
	pairingManager *PairingManager // Reference to the pairing manager for code access
}

// NewPairingDisplay creates a new PairingDisplay instance
func NewPairingDisplay(pairingManager *PairingManager) *PairingDisplay {
	pd := &PairingDisplay{
		templatePath:   getTemplatePath(),
		pairingManager: pairingManager, // Store the pairing manager reference
	}
	return pd
}

// getTemplatePath returns the path to the pairing display template
func getTemplatePath() string {
	// Check if custom template path is set via environment variable
	if customPath := os.Getenv("MSC_TEMPLATE_PATH"); customPath != "" {
		templatePath := filepath.Join(customPath, "pairing_display.html")
		log.Printf("Using custom template path: %s", templatePath)
		return templatePath
	}

	// Default to templates directory relative to executable
	execDir, err := os.Executable()
	if err == nil {
		templatePath := filepath.Join(filepath.Dir(execDir), "templates", "pairing_display.html")
		if _, err := os.Stat(templatePath); err == nil {
			return templatePath
		}
	}

	// Fallback to current working directory
	templatePath := filepath.Join("templates", "pairing_display.html")
	log.Printf("Using fallback template path: %s", templatePath)
	return templatePath
}

// LoadTemplate loads and parses the HTML template
func (pd *PairingDisplay) LoadTemplate() error {
	tmpl, err := template.ParseFiles(pd.templatePath)
	if err != nil {
		return fmt.Errorf("failed to parse template from %s: %w", pd.templatePath, err)
	}
	pd.template = tmpl
	return nil
}

// GetTemplate returns the loaded template, loading it if necessary
func (pd *PairingDisplay) GetTemplate() (*template.Template, error) {
	if pd.template == nil {
		if err := pd.LoadTemplate(); err != nil {
			return nil, err
		}
	}
	return pd.template, nil
}

// ReloadTemplate forces a reload of the template from disk
func (pd *PairingDisplay) ReloadTemplate() error {
	pd.template = nil
	return pd.LoadTemplate()
}

// GenerateQRCode generates a QR code PNG for the given pairing code
func (pd *PairingDisplay) GenerateQRCode(code string) ([]byte, error) {
	// Generate QR code as PNG containing the pairing code
	// The QR code will contain just the pairing code string (e.g., "ABC123")
	png, err := qrcode.Encode(code, qrcode.Medium, 256)
	if err != nil {
		return nil, err
	}
	return png, nil
}

// TemplateData represents the data passed to the template
type TemplateData struct {
	Code        string
	QRCodeImage string
	Expiry      string
	IsExpired   bool
	HasCode     bool
}

// GetTemplateData retrieves the current pairing code data for template rendering
func (pd *PairingDisplay) GetTemplateData() *TemplateData {
	currentCode, currentExpiry := pd.pairingManager.GetPairingCode()

	data := &TemplateData{}

	// Check if we have a valid code
	if currentCode != "" {
		data.HasCode = true
		data.Code = currentCode
		data.Expiry = currentExpiry.Local().Format("Jan 2, 2006 3:04:05 PM")
		data.IsExpired = time.Now().After(currentExpiry)

		// Generate QR code containing just the pairing code
		if qrCodeData, err := pd.GenerateQRCode(currentCode); err == nil {
			data.QRCodeImage = base64.StdEncoding.EncodeToString(qrCodeData)
		}
	}

	return data
}

// HandleQRCodeDisplay handles the pairing display page with QR code
func (pd *PairingDisplay) HandleQRCodeDisplay(cfg config.ClientConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set content type to HTML
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		// Add cache control headers to ensure fresh content
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")

		// Get template data
		data := pd.GetTemplateData()

		// Get template
		tmpl, err := pd.GetTemplate()
		if err != nil {
			log.Printf("Template loading error from %s: %v", pd.templatePath, err)
			// Fallback to a simple error message
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "<html><body><h1>Template Error</h1><p>Could not load pairing display template from: %s</p><p>Error: %v</p></body></html>", pd.templatePath, err)
			return
		}

		if err := tmpl.Execute(w, data); err != nil {
			log.Printf("Template execution error: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}
}
