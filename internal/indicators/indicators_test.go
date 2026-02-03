package indicators

import (
	"math"
	"testing"
)

func TestSMA(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		period   int
		expected float64
	}{
		{
			name:     "Not enough data",
			values:   []float64{1, 2, 3},
			period:   5,
			expected: 2.0, // Average of available
		},
		{
			name:     "Exact period",
			values:   []float64{1, 2, 3, 4, 5},
			period:   5,
			expected: 3.0,
		},
		{
			name:     "More data than period",
			values:   []float64{1, 2, 3, 4, 5, 6, 7},
			period:   5,
			expected: 5.0, // (3+4+5+6+7)/5 = 25/5 = 5
		},
		{
			name:     "Empty",
			values:   []float64{},
			period:   5,
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SMA(tt.values, tt.period)
			if math.Abs(got-tt.expected) > 0.0001 {
				t.Errorf("SMA() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestEMA(t *testing.T) {
	// EMA calculation:
	// Multiplier = 2 / (period + 1)
	// EMA = (Close - EMA_prev) * Multiplier + EMA_prev
	// First EMA is SMA

	tests := []struct {
		name     string
		values   []float64
		period   int
		expected float64
	}{
		{
			name:     "Not enough data",
			values:   []float64{1, 2, 3},
			period:   5,
			expected: 2.0, // Average of available
		},
		{
			name:   "Simple calculation",
			values: []float64{1, 2, 3, 4, 5},
			period: 3,
			// Period 3. Multiplier = 2/4 = 0.5
			// First SMA (1,2,3) = 2.0
			// Next val 4: (4 - 2.0) * 0.5 + 2.0 = 1.0 + 2.0 = 3.0
			// Next val 5: (5 - 3.0) * 0.5 + 3.0 = 1.0 + 3.0 = 4.0
			expected: 4.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EMA(tt.values, tt.period)
			if math.Abs(got-tt.expected) > 0.0001 {
				t.Errorf("EMA() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRSI(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		period   int
		expected float64
	}{
		{
			name:     "Not enough data",
			values:   []float64{1, 2, 3},
			period:   14,
			expected: 50.0,
		},
		{
			name:   "All gains",
			values: []float64{10, 11, 12, 13, 14, 15},
			period: 5,
			// Gains: 1, 1, 1, 1, 1
			// Losses: 0, 0, 0, 0, 0
			// AvgGain: 1, AvgLoss: 0
			// RS -> infinity
			// RSI = 100
			expected: 100.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RSI(tt.values, tt.period)
			if math.Abs(got-tt.expected) > 0.0001 {
				t.Errorf("RSI() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBollingerBands(t *testing.T) {
	tests := []struct {
		name       string
		values     []float64
		period     int
		multiplier float64
		wantUpper  float64
		wantMid    float64
		wantLower  float64
	}{
		{
			name:       "Not enough data",
			values:     []float64{1, 2},
			period:     5,
			multiplier: 2.0,
			wantUpper:  0,
			wantMid:    0,
			wantLower:  0,
		},
		{
			name:   "Standard case",
			values: []float64{10, 10, 10, 10, 10},
			period: 5,
			// Mean 10, StdDev 0
			multiplier: 2.0,
			wantUpper:  10.0,
			wantMid:    10.0,
			wantLower:  10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, m, l := BollingerBands(tt.values, tt.period, tt.multiplier)
			if math.Abs(u-tt.wantUpper) > 0.0001 || math.Abs(m-tt.wantMid) > 0.0001 || math.Abs(l-tt.wantLower) > 0.0001 {
				t.Errorf("BollingerBands() = %v, %v, %v; want %v, %v, %v", u, m, l, tt.wantUpper, tt.wantMid, tt.wantLower)
			}
		})
	}
}

func TestVolume24hAverage(t *testing.T) {
	klines := []Kline{
		{Volume: 100},
		{Volume: 200},
		{Volume: 300},
	}
	expected := 200.0
	got := Volume24hAverage(klines)
	if got != expected {
		t.Errorf("Volume24hAverage() = %v, want %v", got, expected)
	}

	if Volume24hAverage([]Kline{}) != 0 {
		t.Errorf("Volume24hAverage([]Kline{}) != 0")
	}
}
