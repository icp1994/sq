// Package loz contains functionality supplemental to samber/lo.
// Ideally these functions would be merged into that package.
package loz

// All returns a new slice containing elems.
func All[T any](elems ...T) []T {
	a := make([]T, len(elems))
	copy(a, elems)
	return a
}

// Apply returns a new slice whose elements are the result of applying fn to
// each element of collection.
func Apply[T any](collection []T, fn func(item T) T) []T {
	a := make([]T, len(collection))
	for i := range collection {
		a[i] = fn(collection[i])
	}
	return a
}

// ToSliceType returns a new slice of type T, having performed
// type conversion on each element of in.
func ToSliceType[S, T any](in ...S) (out []T, ok bool) {
	out = make([]T, len(in))
	var a any
	for i := range in {
		a = in[i]
		out[i], ok = a.(T)
		if !ok {
			return nil, false
		}
	}

	return out, true
}

// AlignSliceLengths returns slices for a and b such that the
// returned slices have the same lengths and contents as a and b,
// with any additional elements filled with defaultVal. If a and b
// are already the same length, they are returned as-is. At most
// one new slice is allocated.
func AlignSliceLengths[T any](a, b []T, defaultVal T) ([]T, []T) {
	switch {
	case len(a) == len(b):
		return a, b
	case len(a) < len(b):
		a1 := make([]T, len(b))
		for i := range a1 {
			if i < len(a) {
				a1[i] = a[i]
			} else {
				a1[i] = defaultVal
			}
		}
		return a1, b
	default:
		b1 := make([]T, len(a))
		for i := range b1 {
			if i < len(b) {
				b1[i] = b[i]
			} else {
				b1[i] = defaultVal
			}
		}
		return a, b1
	}
}

// AlignMatrixWidth ensures that rows of matrix have the same
// length, that length being the length of the longest row of a.
func AlignMatrixWidth[T any](a [][]T, defaultVal T) {
	if len(a) == 0 {
		return
	}

	var ragged bool
	maxLen := len(a[0])
	for i := 0; i < len(a); i++ {
		if len(a[i]) != maxLen {
			ragged = true
			maxLen = max(len(a[i]), maxLen)
		}
	}

	if !ragged {
		return
	}

	for i := range a {
		if len(a[i]) == maxLen {
			continue
		}

		row := make([]T, maxLen)
		copy(row, a[i])
		for j := len(a[i]); j < len(row); j++ {
			row[j] = defaultVal
		}
		a[i] = row
	}
}

// Make works like the runtime make func, but it also fills
// the slice with val.
func Make[T any](count int, val T) []T {
	a := make([]T, count)
	for i := range a {
		a[i] = val
	}
	return a
}

// IsSliceZeroed returns true if a is empty or if each element
// of a is the zero value.
func IsSliceZeroed[T comparable](a []T) bool {
	if len(a) == 0 {
		return true
	}

	var zero T
	for i := range a {
		if a[i] != zero {
			return false
		}
	}
	return true
}
