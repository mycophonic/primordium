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

package index_test

import (
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/store/index"
)

// benchValue returns a deterministic 65-byte value from a seed.
func benchValue(seed uint64) []byte {
	val := make([]byte, 65)
	val[0] = uint8(seed%5 + 1)
	binary.BigEndian.PutUint64(val[1:9], seed)

	return val
}

func openIndex(b *testing.B) *index.Index {
	b.Helper()

	path := filepath.Join(b.TempDir(), "bench.idx")

	idx, err := index.New(path, nil)
	if err != nil {
		b.Fatalf("open: %v", err)
	}

	b.Cleanup(func() { assert.Check(b, idx.Close()) })

	return idx
}

// BenchmarkPut measures the amortized cost of inserting unique keys,
// including any grow operations that occur along the way.
func BenchmarkPut(b *testing.B) {
	idx := openIndex(b)

	// Pre-compute a value to reuse in the hot loop.
	val := benchValue(0)

	b.ResetTimer()

	for i := range b.N {
		binary.BigEndian.PutUint64(val[1:9], uint64(i))

		if err := idx.Put(uint64(i), val, int64(i)); err != nil {
			b.Fatalf("put %d: %v", i, err)
		}
	}
}

// BenchmarkPutUpdate measures the cost of updating an existing key
// (no growth, no probe misses).
func BenchmarkPutUpdate(b *testing.B) {
	idx := openIndex(b)

	// Seed one key.
	val := benchValue(1)
	if err := idx.Put(42, val, 1); err != nil {
		b.Fatalf("seed: %v", err)
	}

	b.ResetTimer()

	for i := range b.N {
		binary.BigEndian.PutUint64(val[1:9], uint64(i))

		if err := idx.Put(42, val, int64(i)); err != nil {
			b.Fatalf("put %d: %v", i, err)
		}
	}
}

// BenchmarkGet measures lookup cost at various table sizes.
//
//nolint:gocognit // a benchmark: setup, the timed loop and its checks stay in one body
func BenchmarkGet(b *testing.B) {
	for _, count := range []int{100, 1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("n=%d", count), func(b *testing.B) {
			idx := openIndex(b)

			for i := range count {
				val := benchValue(uint64(i))
				if err := idx.Put(uint64(i), val, int64(i)); err != nil {
					b.Fatalf("seed %d: %v", i, err)
				}
			}

			b.ResetTimer()

			for i := range b.N {
				key := uint64(i % count)

				_, found, err := idx.Get(key)
				if err != nil {
					b.Fatalf("get: %v", err)
				}

				if !found {
					b.Fatalf("key %d not found", key)
				}
			}
		})
	}
}

// BenchmarkGetMiss measures lookup cost for keys that do not exist.
func BenchmarkGetMiss(b *testing.B) {
	idx := openIndex(b)

	// Fill with even keys only.
	for i := range 10_000 {
		val := benchValue(uint64(i))
		if err := idx.Put(uint64(i*2), val, int64(i)); err != nil {
			b.Fatalf("seed %d: %v", i, err)
		}
	}

	b.ResetTimer()

	for i := range b.N {
		// Odd keys never exist.
		key := uint64(i*2 + 1)

		_, found, err := idx.Get(key)
		if err != nil {
			b.Fatalf("get: %v", err)
		}

		if found {
			b.Fatalf("key %d should not exist", key)
		}
	}
}

// BenchmarkDelete measures delete cost (marking tombstones).
//
//nolint:gocognit // a benchmark: setup, the timed loop and its checks stay in one body
func BenchmarkDelete(b *testing.B) {
	idx := openIndex(b)

	// Keys are spread, as hashed keys are. Sequential keys fill one unbroken
	// probe run, and every untimed refill Put below would walk it to its end:
	// the refill turns quadratic, and the benchmark runs for minutes while
	// reporting only the deletes.
	count := 10_000
	rng := rand.New(rand.NewPCG(1, 2)) // #nosec G404 -- fixed-seed keys for a repeatable run

	keys := make([]uint64, count)
	for i := range keys {
		keys[i] = rng.Uint64()
	}

	for i, key := range keys {
		val := benchValue(key)
		if err := idx.Put(key, val, int64(i)); err != nil {
			b.Fatalf("seed %d: %v", i, err)
		}
	}

	b.ResetTimer()

	for i := range b.N {
		key := keys[i%count]

		// Re-insert if already deleted so we always have something to delete.
		if i > 0 && i%count == 0 {
			b.StopTimer()

			for j := range keys {
				val := benchValue(keys[j])
				if err := idx.Put(keys[j], val, int64(j)); err != nil {
					b.Fatalf("refill %d: %v", j, err)
				}
			}

			b.StartTimer()
		}

		_, err := idx.Delete(key)
		if err != nil {
			b.Fatalf("delete: %v", err)
		}
	}
}

// BenchmarkForEach measures full iteration cost at various sizes.
//
//nolint:gocognit // a benchmark: setup, the timed loop and its checks stay in one body
func BenchmarkForEach(b *testing.B) {
	for _, count := range []int{100, 1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("n=%d", count), func(b *testing.B) {
			idx := openIndex(b)

			for i := range count {
				val := benchValue(uint64(i))
				if err := idx.Put(uint64(i), val, int64(i)); err != nil {
					b.Fatalf("seed %d: %v", i, err)
				}
			}

			b.ResetTimer()

			for range b.N {
				seen := 0

				err := idx.ForEach(func(_ index.Record) bool {
					seen++

					return true
				})
				if err != nil {
					b.Fatalf("foreach: %v", err)
				}

				if seen != count {
					b.Fatalf("expected %d records, saw %d", count, seen)
				}
			}
		})
	}
}

// BenchmarkGrowth measures the cost of growing from default capacity (1024)
// to a target size. Reports total time and ns/op for the entire fill.
//
//nolint:gocognit // a benchmark: setup, the timed loop and its checks stay in one body
func BenchmarkGrowth(b *testing.B) {
	for _, target := range []int{1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("to=%d", target), func(b *testing.B) {
			val := benchValue(0)

			for range b.N {
				b.StopTimer()

				path := filepath.Join(b.TempDir(), "grow.idx")

				idx, err := index.New(path, nil)
				if err != nil {
					b.Fatalf("open: %v", err)
				}

				b.StartTimer()

				for i := range target {
					binary.BigEndian.PutUint64(val[1:9], uint64(i))

					if err := idx.Put(uint64(i), val, int64(i)); err != nil {
						b.Fatalf("put %d: %v", i, err)
					}
				}

				b.StopTimer()

				assert.Check(b, idx.Close())
			}
		})
	}
}

// BenchmarkDeleteInsertChurn measures put/delete churn that generates
// tombstones and eventually triggers growth from tombstone pressure.
func BenchmarkDeleteInsertChurn(b *testing.B) {
	idx := openIndex(b)

	// Fill to ~50% of default capacity.
	for i := range 500 {
		val := benchValue(uint64(i))
		if err := idx.Put(uint64(i), val, int64(i)); err != nil {
			b.Fatalf("seed %d: %v", i, err)
		}
	}

	val := benchValue(0)

	b.ResetTimer()

	for i := range b.N {
		key := uint64(i % 500)

		// Delete then re-insert with a different value — creates tombstone churn.
		if _, err := idx.Delete(key); err != nil {
			b.Fatalf("delete %d: %v", key, err)
		}

		binary.BigEndian.PutUint64(val[1:9], uint64(i))

		if err := idx.Put(key+500, val, int64(i)); err != nil {
			b.Fatalf("put %d: %v", key+500, err)
		}
	}
}

// BenchmarkSync measures the cost of flushing the mmap'd region.
func BenchmarkSync(b *testing.B) {
	idx := openIndex(b)

	// Put some data so the region isn't trivially empty.
	for i := range 1_000 {
		val := benchValue(uint64(i))
		if err := idx.Put(uint64(i), val, int64(i)); err != nil {
			b.Fatalf("seed %d: %v", i, err)
		}
	}

	b.ResetTimer()

	for range b.N {
		if err := idx.Sync(); err != nil {
			b.Fatalf("sync: %v", err)
		}
	}
}
