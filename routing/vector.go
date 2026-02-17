package routing

import "math"

// cosine returns the cosine similarity between two vectors.
// returns 0 if either vector has zero magnitude.
func cosine(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, magA, magB float64
	for i := range a {
		dot += a[i] * b[i]
		magA += a[i] * a[i]
		magB += b[i] * b[i]
	}

	if magA == 0 || magB == 0 {
		return 0
	}

	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}

// centroid returns the element-wise average of the given vectors.
func centroid(vectors [][]float64) []float64 {
	if len(vectors) == 0 {
		return nil
	}

	dim := len(vectors[0])
	result := make([]float64, dim)

	for _, v := range vectors {
		for i := range v {
			result[i] += v[i]
		}
	}

	n := float64(len(vectors))
	for i := range result {
		result[i] /= n
	}

	return result
}

// normalize returns a unit-length copy of the given vector.
// returns a zero-length slice if the vector has zero magnitude.
func normalize(v []float64) []float64 {
	var mag float64
	for _, x := range v {
		mag += x * x
	}
	mag = math.Sqrt(mag)

	if mag == 0 {
		return make([]float64, len(v))
	}

	result := make([]float64, len(v))
	for i, x := range v {
		result[i] = x / mag
	}
	return result
}
