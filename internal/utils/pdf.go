package utils

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// isScannedPDF detecta si un PDF es probablemente escaneado (sin fuentes en el primer megabyte)
func isScannedPDF(filePath string) bool {
	file, err := os.Open(filePath)
	if err != nil {
		return true // Por seguridad, asumimos escaneado si hay error
	}
	defer file.Close()

	// Leer el primer megabyte
	buffer := make([]byte, 1024*1024)
	n, _ := file.Read(buffer)
	if n == 0 {
		return true
	}

	content := string(buffer[:n])
	// Comprobar si tiene indicaciones de texto o fuentes digitalizadas
	if strings.Contains(content, "/Font") || strings.Contains(content, "/Type /Font") || strings.Contains(content, "/Type/Font") || strings.Contains(content, "BT") {
		return false // PDF Digital con fuentes/texto
	}

	return true // PDF Escaneado (solo imágenes)
}

// CompressPDF reduce el tamaño de un PDF usando Ghostscript
func CompressPDF(ctx context.Context, inputPath, outputPath string) error {
	// Detectar el ejecutable según el sistema operativo
	executable := "gs"
	if runtime.GOOS == "windows" {
		executable = "gswin64c" // Nombre del ejecutable de consola en Windows
	}

	// Determinar el DPI según si el documento es digital o escaneado
	targetDPI := "130" // Para PDFs digitales (texto nativo)
	if isScannedPDF(inputPath) {
		targetDPI = "200" // Mayor calidad/resolución para PDFs escaneados (imágenes legibles)
	}

	cmd := exec.CommandContext(ctx, executable,
		"-sDEVICE=pdfwrite",
		"-dCompatibilityLevel=1.4",
		"-dNOPAUSE",
		"-dQUIET",
		"-dBATCH",
		"-dDownsampleColorImages=true",
		"-dColorImageResolution="+targetDPI,
		"-dColorImageDownsampleThreshold=1.0",
		"-dDownsampleGrayImages=true",
		"-dGrayImageResolution="+targetDPI,
		"-dGrayImageDownsampleThreshold=1.0",
		"-dDownsampleMonoImages=true",
		"-dMonoImageResolution="+targetDPI,
		"-dMonoImageDownsampleThreshold=1.0",
		"-sOutputFile="+outputPath,
		inputPath,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error al ejecutar ghostscript (%s): %w. Detalle: %s", executable, err, stderr.String())
	}

	return nil
}
