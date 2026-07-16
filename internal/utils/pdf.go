package utils

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// CompressPDF reduce el tamaño de un PDF usando Ghostscript
func CompressPDF(ctx context.Context, inputPath, outputPath string) error {
	// Detectar el ejecutable según el sistema operativo
	executable := "gs"
	if runtime.GOOS == "windows" {
		executable = "gswin64c" // Nombre del ejecutable de consola en Windows
	}

	// Calidad de compresión personalizada (DPI):
	// Ajusta este valor para afinar la relación tamaño/legibilidad (ej. 100, 110, 120)
	targetDPI := "180"

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
