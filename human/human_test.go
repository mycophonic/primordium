/*
   Copyright Mycophonic.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package human_test

import (
	"fmt"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/bytesize"
	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/human"
)

func ExampleDecimalSize() {
	fmt.Println(human.DecimalSize(1000))
	fmt.Println(human.DecimalSize(1024))
	fmt.Println(human.DecimalSize(1048576))
	fmt.Println(human.DecimalSize(3.42 * bytesize.GB))
	// Output:
	// 1kB
	// 1.024kB
	// 1.049MB
	// 3.42GB
}

func ExampleBinarySize() {
	fmt.Println(human.BinarySize(1024))
	fmt.Println(human.BinarySize(2 * bytesize.MiB))
	fmt.Println(human.BinarySize(3.42 * bytesize.GiB))
	// Output:
	// 1KiB
	// 2MiB
	// 3.42GiB
}

func ExampleParseSize() {
	fmt.Println(human.ParseSize("32"))
	fmt.Println(human.ParseSize("32kB"))
	fmt.Println(human.ParseSize("32KiB"))
	fmt.Println(human.ParseSize("32.5 MB"))
	// Output:
	// 32 <nil>
	// 32000 <nil>
	// 32768 <nil>
	// 32500000 <nil>
}

func TestDecimalSize(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		size float64
		want string
	}{
		{0, "0B"},
		{1000, "1kB"},
		{1024, "1.024kB"},
		{1000000, "1MB"},
		{1048576, "1.049MB"},
		{2 * bytesize.MB, "2MB"},
		{3.42 * bytesize.GB, "3.42GB"},
		{5.372 * bytesize.TB, "5.372TB"},
		{2.22 * bytesize.PB, "2.22PB"},
		{10000000000000 * bytesize.PB, "1e+04YB"},
	} {
		assert.Equal(t, human.DecimalSize(tc.size), tc.want, "DecimalSize(%v)", tc.size)
	}
}

func TestDecimalSizeWithPrecision(t *testing.T) {
	t.Parallel()

	assert.Equal(t, human.DecimalSizeWithPrecision(1048576, 3), "1.05MB")
	assert.Equal(t, human.DecimalSizeWithPrecision(1048576, 6), "1.04858MB")
	assert.Equal(t, human.DecimalSizeWithPrecision(1000, 1), "1kB")
}

func TestBinarySizeWithPrecision(t *testing.T) {
	t.Parallel()

	// 1000000 bytes are 976.5625KiB.
	assert.Equal(t, human.BinarySizeWithPrecision(1000000, 3), "977KiB")
	assert.Equal(t, human.BinarySizeWithPrecision(1000000, 4), "976.6KiB")
	assert.Equal(t, human.BinarySizeWithPrecision(1536, 2), "1.5KiB")
	assert.Equal(t, human.BinarySizeWithPrecision(1024, 1), "1KiB")
}

func TestBinarySize(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		size float64
		want string
	}{
		{0, "0B"},
		{1024, "1KiB"},
		{1024 * 1024, "1MiB"},
		{2 * bytesize.MiB, "2MiB"},
		{3.42 * bytesize.GiB, "3.42GiB"},
		{5.372 * bytesize.TiB, "5.372TiB"},
		{2.22 * bytesize.PiB, "2.22PiB"},
		{bytesize.KiB * bytesize.KiB * bytesize.KiB * bytesize.KiB * bytesize.KiB * bytesize.PiB, "1.049e+06YiB"},
	} {
		assert.Equal(t, human.BinarySize(tc.size), tc.want, "BinarySize(%v)", tc.size)
	}
}

func TestParseSize(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		size string
		want int64
	}{
		{"0", 0},
		{"32", 32},
		{"32B", 32},
		{"32 B", 32},
		{"32.5 B", 32},
		{"32kB", 32 * bytesize.KB},
		{"32 kB", 32 * bytesize.KB},
		{"32.5kB", 32500},
		{"32MB", 32 * bytesize.MB},
		{"32GB", 32 * bytesize.GB},
		{"32TB", 32 * bytesize.TB},
		{"32PB", 32 * bytesize.PB},
		{"8EB", 8 * bytesize.EB},
		{"32KiB", 32 * bytesize.KiB},
		{"32 KiB", 32 * bytesize.KiB},
		{"32MiB", 32 * bytesize.MiB},
		{"32GiB", 32 * bytesize.GiB},
		{"32TiB", 32 * bytesize.TiB},
		{"32PiB", 32 * bytesize.PiB},
		{"7EiB", 7 * bytesize.EiB},
		// Case does not matter; the letters decide the base.
		{"32b", 32},
		{"32KB", 32 * bytesize.KB},
		{"32kb", 32 * bytesize.KB},
		{"32mb", 32 * bytesize.MB},
		{"32Mb", 32 * bytesize.MB},
		{"32MIB", 32 * bytesize.MiB},
		{"32mib", 32 * bytesize.MiB},
		{"32kiB", 32 * bytesize.KiB},
		// 32.3 MiB is 33869004.8 bytes and 0.3 MiB 314572.8; the fraction is truncated.
		{"32.3MiB", 33869004},
		{"0.3 MiB", 314572},
		// 8 EiB less 1 KiB: the largest multiple of 1 KiB below 2^63 bytes.
		{"9007199254740991KiB", 9007199254740991 * bytesize.KiB},
	} {
		got, err := human.ParseSize(tc.size)
		assert.NilError(t, err, "ParseSize(%q)", tc.size)
		assert.Equal(t, got, tc.want, "ParseSize(%q)", tc.size)
	}
}

func TestParseSizeInvalid(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		size string
		want error
	}{
		// A unit is a whole symbol.
		{"32k", human.ErrInvalidUnit},
		{"32K", human.ErrInvalidUnit},
		{"32M", human.ErrInvalidUnit},
		{"32Ki", human.ErrInvalidUnit},
		{"32ZB", human.ErrInvalidUnit},
		{"32YiB", human.ErrInvalidUnit},
		{"32 bytes", human.ErrInvalidUnit},
		{"32MBB", human.ErrInvalidUnit},
		// One space between the number and a unit, no other.
		{"32 ", human.ErrInvalidUnit},
		{"32  kB", human.ErrInvalidUnit},
		{"32kB ", human.ErrInvalidUnit},
		{"32 kB B", human.ErrInvalidUnit},
		{" 32", human.ErrInvalidSize},
		{" 32kB", human.ErrInvalidSize},
		// A number is digits with an optional fraction.
		{"", human.ErrInvalidSize},
		{"kB", human.ErrInvalidSize},
		{"hello", human.ErrInvalidSize},
		{".", human.ErrInvalidSize},
		{".5kB", human.ErrInvalidSize},
		{"32.", human.ErrInvalidSize},
		{"32.kB", human.ErrInvalidSize},
		{"3.2.1kB", human.ErrInvalidSize},
		{"-32", human.ErrInvalidSize},
		{"-0", human.ErrInvalidSize},
		{"+32", human.ErrInvalidSize},
		{"1e3", human.ErrInvalidUnit},
		{"0x10", human.ErrInvalidUnit},
		{"1_000", human.ErrInvalidUnit},
		// Sizes an int64 cannot count.
		{"9223372036854775808", human.ErrInvalidSize},
		{"10000000PB", human.ErrInvalidSize},
		{"8EiB", human.ErrInvalidSize},
	} {
		got, err := human.ParseSize(tc.size)
		assert.ErrorIs(t, err, tc.want, "ParseSize(%q)", tc.size)
		assert.ErrorIs(t, err, fault.ErrInvalidArgument, "ParseSize(%q)", tc.size)
		assert.Equal(t, got, int64(0), "ParseSize(%q)", tc.size)
	}
}

// What the formatters print, ParseSize reads back, to the four significant
// digits they keep.
func TestParseSizeReadsFormattedSizes(t *testing.T) {
	t.Parallel()

	for _, size := range []float64{
		0, 1, 999, 1000, 1024, 1536, 123456, 2 * bytesize.MiB, 3.42 * bytesize.GB, 5.372 * bytesize.TiB, 7 * bytesize.EB,
	} {
		for _, formatted := range []string{human.DecimalSize(size), human.BinarySize(size)} {
			got, err := human.ParseSize(formatted)
			assert.NilError(t, err, "ParseSize(%q)", formatted)

			tolerance := size / 1000
			assert.Assert(t, float64(got) >= size-tolerance-1 && float64(got) <= size+tolerance,
				"ParseSize(%q) = %d, want %v within %v", formatted, got, size, tolerance)
		}
	}
}

func BenchmarkParseSize(b *testing.B) {
	sizes := []string{"", "32", "32B", "32 B", "32kB", "32.5 kB", "32KiB", "32.8MB", "32.9GiB", "0.3MiB", "-1", "32mb"}

	for range b.N {
		for _, size := range sizes {
			_, _ = human.ParseSize(size)
		}
	}
}
