package routing

import (
	"math"
	"testing"
)

func TestCosineIdenticalVectors(t *testing.T) {
	a := []float64{1, 2, 3}
	if got := cosine(a, a); math.Abs(got-1.0) > 1e-9 {
		t.Errorf("cosine(a, a) = %f, want 1.0", got)
	}
}

func TestCosineOrthogonalVectors(t *testing.T) {
	a := []float64{1, 0, 0}
	b := []float64{0, 1, 0}
	if got := cosine(a, b); math.Abs(got) > 1e-9 {
		t.Errorf("cosine(orthogonal) = %f, want 0.0", got)
	}
}

func TestCosineOppositeVectors(t *testing.T) {
	a := []float64{1, 2, 3}
	b := []float64{-1, -2, -3}
	if got := cosine(a, b); math.Abs(got+1.0) > 1e-9 {
		t.Errorf("cosine(opposite) = %f, want -1.0", got)
	}
}

func TestCosineDifferentLengths(t *testing.T) {
	a := []float64{1, 2}
	b := []float64{1, 2, 3}
	if got := cosine(a, b); got != 0 {
		t.Errorf("cosine(different lengths) = %f, want 0", got)
	}
}

func TestCosineEmpty(t *testing.T) {
	if got := cosine(nil, nil); got != 0 {
		t.Errorf("cosine(nil, nil) = %f, want 0", got)
	}
}

func TestCosineZeroVector(t *testing.T) {
	a := []float64{0, 0, 0}
	b := []float64{1, 2, 3}
	if got := cosine(a, b); got != 0 {
		t.Errorf("cosine(zero, b) = %f, want 0", got)
	}
}

func TestCentroid(t *testing.T) {
	vectors := [][]float64{
		{1, 2, 3},
		{3, 4, 5},
	}
	got := centroid(vectors)
	want := []float64{2, 3, 4}
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Errorf("centroid[%d] = %f, want %f", i, got[i], want[i])
		}
	}
}

func TestCentroidSingle(t *testing.T) {
	vectors := [][]float64{{5, 10, 15}}
	got := centroid(vectors)
	want := []float64{5, 10, 15}
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Errorf("centroid[%d] = %f, want %f", i, got[i], want[i])
		}
	}
}

func TestCentroidEmpty(t *testing.T) {
	if got := centroid(nil); got != nil {
		t.Errorf("centroid(nil) = %v, want nil", got)
	}
}

func TestNormalize(t *testing.T) {
	v := []float64{3, 4}
	got := normalize(v)
	// magnitude should be 1.0
	var mag float64
	for _, x := range got {
		mag += x * x
	}
	mag = math.Sqrt(mag)
	if math.Abs(mag-1.0) > 1e-9 {
		t.Errorf("normalize magnitude = %f, want 1.0", mag)
	}
	// direction preserved
	if math.Abs(got[0]-0.6) > 1e-9 || math.Abs(got[1]-0.8) > 1e-9 {
		t.Errorf("normalize([3,4]) = %v, want [0.6, 0.8]", got)
	}
}

func TestNormalizeZero(t *testing.T) {
	v := []float64{0, 0, 0}
	got := normalize(v)
	for i, x := range got {
		if x != 0 {
			t.Errorf("normalize(zero)[%d] = %f, want 0", i, x)
		}
	}
}
