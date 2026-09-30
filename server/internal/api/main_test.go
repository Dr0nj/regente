package api

import (
	"os"
	"testing"
	"time"
)

// Fixtures históricas que usam time.Now/Local rodam numa zona explícita.
// TestI06HostTimezoneIndependence cobre hosts diferentes da zona de negócio.
func TestMain(m *testing.M) { time.Local = time.UTC; os.Exit(m.Run()) }
