package indicators

import (
	"math"
)

// Kline represents OHLCV candlestick data
type Kline struct {
	OpenTime  int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	CloseTime int64
}

// RSI calculates Relative Strength Index
// period is typically 14
func RSI(closes []float64, period int) float64 {
	if len(closes) < period+1 {
		return 50 // Not enough data, return neutral
	}

	// Calculate price changes
	gains := make([]float64, 0, len(closes)-1)
	losses := make([]float64, 0, len(closes)-1)

	for i := 1; i < len(closes); i++ {
		change := closes[i] - closes[i-1]
		if change > 0 {
			gains = append(gains, change)
			losses = append(losses, 0)
		} else {
			gains = append(gains, 0)
			losses = append(losses, -change)
		}
	}

	// Calculate initial average gain and loss
	avgGain := average(gains[:period])
	avgLoss := average(losses[:period])

	// Smooth averages using Wilder's method
	for i := period; i < len(gains); i++ {
		avgGain = (avgGain*(float64(period)-1) + gains[i]) / float64(period)
		avgLoss = (avgLoss*(float64(period)-1) + losses[i]) / float64(period)
	}

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

// EMA calculates Exponential Moving Average
func EMA(values []float64, period int) float64 {
	if len(values) < period {
		return average(values)
	}

	multiplier := 2.0 / float64(period+1)
	ema := average(values[:period]) // Start with SMA

	for i := period; i < len(values); i++ {
		ema = (values[i]-ema)*multiplier + ema
	}

	return ema
}

// SMA calculates Simple Moving Average
func SMA(values []float64, period int) float64 {
	if len(values) < period {
		return average(values)
	}
	return average(values[len(values)-period:])
}

// MACD calculates Moving Average Convergence Divergence
// Returns: macdLine, signalLine, histogram
func MACD(closes []float64, fastPeriod, slowPeriod, signalPeriod int) (float64, float64, float64) {
	if len(closes) < slowPeriod {
		return 0, 0, 0
	}

	// Calculate MACD line (fast EMA - slow EMA)
	fastEMA := emaValues(closes, fastPeriod)
	slowEMA := emaValues(closes, slowPeriod)

	macdLine := make([]float64, len(slowEMA))
	startIdx := len(fastEMA) - len(slowEMA)
	for i := 0; i < len(slowEMA); i++ {
		macdLine[i] = fastEMA[startIdx+i] - slowEMA[i]
	}

	// Calculate signal line (EMA of MACD line)
	signalLine := EMA(macdLine, signalPeriod)
	currentMACD := macdLine[len(macdLine)-1]
	histogram := currentMACD - signalLine

	return currentMACD, signalLine, histogram
}

// BollingerBands calculates Bollinger Bands
// Returns: upper, middle (SMA), lower
func BollingerBands(closes []float64, period int, stdDevMultiplier float64) (float64, float64, float64) {
	if len(closes) < period {
		return 0, 0, 0
	}

	middle := SMA(closes, period)
	stdDev := standardDeviation(closes[len(closes)-period:])

	upper := middle + (stdDev * stdDevMultiplier)
	lower := middle - (stdDev * stdDevMultiplier)

	return upper, middle, lower
}

// ATR calculates Average True Range
func ATR(klines []Kline, period int) float64 {
	if len(klines) < period+1 {
		return 0
	}

	trueRanges := make([]float64, 0, len(klines)-1)

	for i := 1; i < len(klines); i++ {
		high := klines[i].High
		low := klines[i].Low
		prevClose := klines[i-1].Close

		tr := math.Max(high-low, math.Max(math.Abs(high-prevClose), math.Abs(low-prevClose)))
		trueRanges = append(trueRanges, tr)
	}

	// Use EMA for ATR (Wilder's smoothing)
	return EMA(trueRanges, period)
}

// Volume24hAverage calculates 24h volume average
func Volume24hAverage(klines []Kline) float64 {
	if len(klines) == 0 {
		return 0
	}

	total := 0.0
	for _, k := range klines {
		total += k.Volume
	}
	return total / float64(len(klines))
}

// Helper functions

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func emaValues(values []float64, period int) []float64 {
	if len(values) < period {
		return values
	}

	result := make([]float64, len(values)-period+1)
	multiplier := 2.0 / float64(period+1)

	// Start with SMA
	result[0] = average(values[:period])

	// Calculate subsequent EMAs
	for i := period; i < len(values); i++ {
		result[i-period+1] = (values[i]-result[i-period])*multiplier + result[i-period]
	}

	return result
}

func standardDeviation(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	mean := average(values)
	sumSquares := 0.0

	for _, v := range values {
		diff := v - mean
		sumSquares += diff * diff
	}

	return math.Sqrt(sumSquares / float64(len(values)))
}
