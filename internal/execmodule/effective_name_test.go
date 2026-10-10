package execmodule

import "testing"

func TestEffectiveName(t *testing.T) {
	tests := []struct {
		name         string
		rawName      string
		manifestPath string
		want         string
	}{
		{"явное имя выигрывает у файла", "x", "/m/deploy.yaml", "x"},
		{"явное имя выигрывает у каталога", "x", "/m/deploy/config.yaml", "x"},
		{"одиночный файл", "", "/m/deploy.yaml", "deploy"},
		{"пакет", "", "/m/deploy/config.yaml", "deploy"},
		{"futi.yaml", "", "/proj/futi.yaml", "futi"},
		{"вложенный пакет", "", "/m/dev/deploy/config.yaml", "deploy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EffectiveName(tt.rawName, tt.manifestPath); got != tt.want {
				t.Fatalf("EffectiveName(%q, %q) = %q, want %q", tt.rawName, tt.manifestPath, got, tt.want)
			}
		})
	}
}
