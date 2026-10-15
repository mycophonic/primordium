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

package index

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/forkcloser/xos"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/filesystem/flock"
	"github.com/mycophonic/primordium/mmap"
)

// Header layout (48 bytes):
//
//	[0:4]   magic
//	[4:8]   version
//	[8:10]  valSize (value field width in bytes)
//	[10:16] reserved
//	[16:24] capacity (number of buckets)
//	[24:32] count (occupied entries)
//	[32:40] tombstones
//	[40:48] lock word (atomic, cross-process RWLock)
type header struct {
	Magic      uint32
	Version    uint32
	ValSize    uint16
	Capacity   uint64
	Count      uint64
	Tombstones uint64
}

// Options configures an Index opened via New.
type Options struct {
	// InitialCap is the number of buckets to allocate on first creation.
	// Zero means defaultCap (1024). Should be a power of two for optimal
	// distribution of keys across buckets (the index uses key % capacity).
	InitialCap uint64
	// MaxCap is the maximum number of buckets the index may grow to.
	// Zero means unlimited.
	MaxCap uint64
	// ValSize is the fixed width in bytes of the opaque value field stored
	// per record. Zero means defaultValSize (65). Must match the value
	// stored in an existing index file; a mismatch is an error.
	ValSize int
}

// Index is a memory-mapped flat-file hash table.
type Index struct {
	mu          sync.RWMutex
	path        string
	lockPath    string
	journalPath string
	dataFile    *os.File
	mapping     *mmap.Mapping
	data        []byte // the mapping's bytes
	cap         uint64
	maxCap      uint64
	valSize     int

	// Lock file: mmap'd reader PID slots for stale-reader detection.
	lockMapping  *mmap.Mapping
	lockData     []byte // the lock mapping's bytes
	lockDataFile *os.File
	localReaders atomic.Int32 // per-process reader goroutine count
	readerSlot   atomic.Int32 // claimed PID slot index, -1 if none
	// slotMu makes a reader-count transition and its slot change one step:
	// without it a goroutine leaving at 1→0 clears the slot a goroutine
	// arriving at 0→1 has just claimed, and that reader is invisible to a
	// writer's stale-reader check.
	slotMu sync.Mutex
}

// Record represents a single entry in the index.
type Record struct {
	Key       uint64
	Value     []byte
	Timestamp int64
}

// New opens or creates an index at the given path.
//
//nolint:gocognit // one setup sequence, in order; its complexity is an error check per step
func New(path string, opts *Options) (*Index, error) {
	idx := &Index{path: path, lockPath: path + ".lock", journalPath: path + ".journal"}

	initialCap := uint64(defaultCap)
	idx.valSize = defaultValSize

	if opts != nil {
		if opts.InitialCap != 0 {
			initialCap = opts.InitialCap
		}

		idx.maxCap = opts.MaxCap

		if opts.ValSize != 0 {
			idx.valSize = opts.ValSize
		}
	}

	if idx.valSize <= 0 || idx.valSize > math.MaxUint16 {
		return nil, fmt.Errorf("%w: valSize %d out of range (1..%d)",
			fault.ErrInvalidArgument, idx.valSize, math.MaxUint16)
	}

	if idx.maxCap != 0 && initialCap > idx.maxCap {
		return nil, fmt.Errorf("%w: initial capacity %d exceeds max %d", ErrCapacityExceeded, initialCap, idx.maxCap)
	}

	// Ensure the parent directory exists.
	if err := os.MkdirAll(filepath.Dir(path), filesystem.DirPermissionsPrivate); err != nil {
		return nil, fmt.Errorf("%w: create index directory: %w", fault.ErrFilesystemFailure, err)
	}

	// Ensure the sidecar lock file exists and is sized for PID slots.
	if err := ensureLockFile(idx.lockPath); err != nil {
		return nil, fmt.Errorf("%w: create lock file: %w", fault.ErrFilesystemFailure, err)
	}

	if err := idx.mmapLockFile(); err != nil {
		return nil, err
	}

	idx.readerSlot.Store(-1)

	// From this point, any error must clean up resources opened so far.
	opened := false

	defer func() {
		if !opened {
			_ = idx.Close()
		}
	}()

	// Exclusive lock for setup only — released before return.
	setupLock, err := flock.Lock(idx.lockPath)
	if err != nil {
		return nil, fmt.Errorf("%w: lock: %w", fault.ErrFilesystemFailure, err)
	}

	// Unlock closes the lock file whatever happens, which releases the lock.
	defer func() { _ = flock.Unlock(setupLock) }()

	_, err = xos.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err = idx.create(initialCap); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, fmt.Errorf("%w: stat data file: %w", fault.ErrFilesystemFailure, err)
	}

	dataFile, err := xos.OpenFile(path, os.O_RDWR, filesystem.FilePermissionsPrivate)
	if err != nil {
		return nil, fmt.Errorf("%w: open data file: %w", fault.ErrFilesystemFailure, err)
	}

	idx.dataFile = dataFile

	// A grow a writer died in has left its journal: the table is rebuilt
	// from it before the header is read, under the setup lock, the way a
	// peer that takes the dead writer's lock rebuilds it, in the file every
	// live holder has mapped. The rebuild maps the file itself, sized to
	// the journal: a file the writer never got to size is too small to map.
	journal, found, err := readJournalFor(idx.journalPath, idx.valSize, idx.maxCap)
	if err != nil {
		return nil, err
	}

	if found {
		err = idx.rebuild(journal)
	} else {
		err = idx.mmapDataFile()
	}

	if err != nil {
		return nil, err
	}

	if found {
		idx.freeDeadWritersWord()
	}

	hdr := idx.readHeader()
	if hdr.Magic != magic {
		return nil, fmt.Errorf("%w: invalid magic: %#x", fault.ErrInvalidArgument, hdr.Magic)
	}

	if hdr.Version != version {
		return nil, fmt.Errorf("%w: unsupported version: %d", fault.ErrInvalidArgument, hdr.Version)
	}

	if hdr.ValSize != uint16(idx.valSize) {
		return nil, fmt.Errorf("%w: valSize mismatch: file has %d, options specify %d",
			fault.ErrInvalidArgument, hdr.ValSize, idx.valSize)
	}

	if idx.maxCap != 0 && hdr.Capacity > idx.maxCap {
		return nil, fmt.Errorf("%w: existing capacity %d exceeds max %d",
			ErrCapacityExceeded, hdr.Capacity, idx.maxCap)
	}

	idx.cap = hdr.Capacity
	opened = true

	return idx, nil
}

// Close syncs the mmap'd region to disk, then unmaps and closes all
// file handles. Safe to use as a shutdown handler via shutdown.Register.
func (idx *Index) Close() error {
	var errs []error

	// Clear our reader PID slot before unmapping the lock file.
	idx.clearReaderSlot()

	if idx.data != nil {
		if err := idx.mapping.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("%w: sync: %w", fault.ErrWriteFailure, err))
		}

		if err := idx.mapping.Unmap(); err != nil {
			errs = append(errs, fmt.Errorf("%w: unmap: %w", fault.ErrFilesystemFailure, err))
		}

		idx.data = nil
		idx.mapping = nil
	}

	if idx.dataFile != nil {
		if err := idx.dataFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("%w: close data: %w", fault.ErrFilesystemFailure, err))
		}

		idx.dataFile = nil
	}

	if idx.lockData != nil {
		if err := idx.lockMapping.Unmap(); err != nil {
			errs = append(errs, fmt.Errorf("%w: unmap lock: %w", fault.ErrFilesystemFailure, err))
		}

		idx.lockData = nil
		idx.lockMapping = nil
	}

	if idx.lockDataFile != nil {
		if err := idx.lockDataFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("%w: close lock: %w", fault.ErrFilesystemFailure, err))
		}

		idx.lockDataFile = nil
	}

	return errors.Join(errs...)
}

// Get retrieves a record by key. Returns false if not found.
func (idx *Index) Get(key uint64) (Record, bool, error) {
	if err := idx.rlockFresh(); err != nil {
		return Record{}, false, err
	}

	defer idx.runlock()

	rec, found := idx.getUnlocked(key)

	return rec, found, nil
}

// Put inserts or updates a record. Grows the table if needed.
// The value slice must be between 1 and valSize bytes; it is zero-padded
// internally to the configured valSize.
//
//nolint:gocognit // the grow check and the probe belong under one write lock, in one place
func (idx *Index) Put(key uint64, value []byte, timestamp int64) error {
	if len(value) == 0 {
		return fmt.Errorf("%w: empty value", fault.ErrInvalidArgument)
	}

	if len(value) > idx.valSize {
		return fmt.Errorf("%w: value too long: %d > %d", fault.ErrInvalidArgument, len(value), idx.valSize)
	}

	val := make([]byte, idx.valSize)
	copy(val, value)

	if err := idx.wlockFresh(); err != nil {
		return err
	}

	defer idx.wunlock()

	hdr := idx.readHeader()

	// Check if we need to grow. Include tombstones — they occupy slots and
	// lengthen probe chains even though they hold no live data.
	if float64(hdr.Count+hdr.Tombstones+1)/float64(hdr.Capacity) > maxLoadFactor {
		if err := idx.growLocked(); err != nil {
			return fmt.Errorf("grow: %w", err)
		}

		hdr = idx.readHeader()
	}

	start := key % hdr.Capacity

	var firstTombstone int64 = -1

	for probe := range hdr.Capacity {
		bucket := (start + probe) % hdr.Capacity
		off := idx.offset(bucket)
		status := idx.data[off]

		switch status {
		case statusEmpty:
			// Insert at tombstone if we passed one, otherwise here.
			if firstTombstone >= 0 {
				off = firstTombstone
				hdr.Tombstones--
			}

			idx.writeRecord(off, key, val, timestamp)

			hdr.Count++
			idx.writeHeader(hdr)

			return nil

		case statusOccupied:
			if idx.readKey(off) == key {
				// Update existing.
				idx.writeRecord(off, key, val, timestamp)

				return nil
			}

		case statusDeleted:
			if firstTombstone < 0 {
				firstTombstone = off
			}
		default:
		}
	}

	// Should not happen if load factor is maintained.
	return fmt.Errorf("%w: table full", fault.ErrSystemFailure)
}

// Delete removes a record by key. Returns false if not found.
func (idx *Index) Delete(key uint64) (bool, error) {
	if err := idx.wlockFresh(); err != nil {
		return false, err
	}

	defer idx.wunlock()

	hdr := idx.readHeader()
	start := key % hdr.Capacity

	for probe := range hdr.Capacity {
		bucket := (start + probe) % hdr.Capacity
		off := idx.offset(bucket)
		status := idx.data[off]

		switch status {
		case statusEmpty:
			return false, nil
		case statusOccupied:
			if idx.readKey(off) == key {
				idx.data[off] = statusDeleted
				hdr.Count--
				hdr.Tombstones++
				idx.writeHeader(hdr)

				return true, nil
			}
		default:
		}
	}

	return false, nil
}

// Len returns the number of occupied entries.
func (idx *Index) Len() (uint64, error) {
	if err := idx.rlockFresh(); err != nil {
		return 0, err
	}

	defer idx.runlock()

	return idx.readHeader().Count, nil
}

// Cap returns the current bucket capacity.
func (idx *Index) Cap() (uint64, error) {
	if err := idx.rlockFresh(); err != nil {
		return 0, err
	}

	defer idx.runlock()

	return idx.readHeader().Capacity, nil
}

// ForEach iterates over all occupied records. The callback must not
// call any Index methods (the read lock is held).
func (idx *Index) ForEach(visit func(Record) bool) error {
	if err := idx.rlockFresh(); err != nil {
		return err
	}

	defer idx.runlock()

	hdr := idx.readHeader()
	for bucket := range hdr.Capacity {
		off := idx.offset(bucket)
		if idx.data[off] == statusOccupied {
			rec := idx.readRecord(off)
			if !visit(rec) {
				return nil
			}
		}
	}

	return nil
}

// Sync flushes the mmap'd region to disk.
func (idx *Index) Sync() error {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if idx.data == nil {
		return nil
	}

	if err := idx.mapping.Sync(); err != nil {
		return fmt.Errorf("%w: %w", fault.ErrWriteFailure, err)
	}

	return nil
}

// --- internal ---

func (idx *Index) recordSize() int64 {
	return 1 + keySize + int64(idx.valSize) + tsSize
}

func ensureLockFile(path string) error {
	lockFile, err := xos.OpenFile(path, os.O_CREATE|os.O_RDWR, filesystem.FilePermissionsPrivate)
	if err != nil {
		//nolint:wrapcheck // *os.PathError already names the file
		return err
	}

	if err = lockFile.Truncate(lockFileSize); err != nil {
		_ = lockFile.Close()

		return fmt.Errorf("%w: truncate lock file: %w", fault.ErrWriteFailure, err)
	}

	if err = lockFile.Close(); err != nil {
		return fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	return nil
}

func (idx *Index) mmapLockFile() error {
	lockFile, err := xos.OpenFile(idx.lockPath, os.O_RDWR, filesystem.FilePermissionsPrivate)
	if err != nil {
		return fmt.Errorf("%w: open lock file for mmap: %w", fault.ErrFilesystemFailure, err)
	}

	mapping, err := mmap.Map(lockFile, lockFileSize)
	if err != nil {
		_ = lockFile.Close()

		return fmt.Errorf("%w: mmap lock file: %w", fault.ErrFilesystemFailure, err)
	}

	idx.lockDataFile = lockFile
	idx.lockMapping = mapping
	idx.lockData = mapping.Bytes()

	return nil
}

func (idx *Index) create(capacity uint64) error {
	file, err := xos.OpenFile(idx.path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, filesystem.FilePermissionsPrivate)
	if err != nil {
		return fmt.Errorf("%w: create data file: %w", fault.ErrFilesystemFailure, err)
	}
	defer func() { _ = file.Close() }() // data synced; a close error is not actionable

	// #nosec G115 -- capacity is bounded by available memory; overflow requires >3.6×10^17 buckets
	size := int64(headerSize) + int64(capacity)*idx.recordSize()
	if err := file.Truncate(size); err != nil {
		return fmt.Errorf("%w: truncate: %w", fault.ErrWriteFailure, err)
	}

	buf := make([]byte, headerSize)
	marshalHeader(buf, header{
		Magic:   magic,
		Version: version,
		// #nosec G115 -- valSize is validated to be in [1, 65535] in New
		ValSize:  uint16(idx.valSize),
		Capacity: capacity,
	})

	if _, err := file.WriteAt(buf, 0); err != nil {
		return fmt.Errorf("%w: write header: %w", fault.ErrWriteFailure, err)
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("%w: sync: %w", fault.ErrWriteFailure, err)
	}

	return nil
}

// writeJournal serializes live records to a journal file so they can be
// recovered if the subsequent in-place grow is interrupted by a crash.
func writeJournal(path string, newCap uint64, valSize int, records []Record) error {
	const journalHeaderSize = 24

	journalRecordSize := keySize + valSize + tsSize

	buf := make([]byte, journalHeaderSize+len(records)*journalRecordSize)
	binary.BigEndian.PutUint64(buf[0:8], newCap)
	binary.BigEndian.PutUint64(buf[8:16], uint64(len(records)))
	// #nosec G115 -- valSize is validated to be in [1, 65535] in New
	binary.BigEndian.PutUint16(buf[16:18], uint16(valSize))

	for i, rec := range records {
		off := journalHeaderSize + i*journalRecordSize
		binary.BigEndian.PutUint64(buf[off:off+keySize], rec.Key)
		copy(buf[off+keySize:off+keySize+valSize], rec.Value)
		// #nosec G115 -- round-trip: int64→uint64 on write, uint64→int64 on read
		binary.BigEndian.PutUint64(buf[off+keySize+valSize:off+keySize+valSize+tsSize], uint64(rec.Timestamp))
	}

	file, err := xos.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, filesystem.FilePermissionsPrivate)
	if err != nil {
		return fmt.Errorf("%w: create journal: %w", fault.ErrFilesystemFailure, err)
	}
	defer func() { _ = file.Close() }() // data synced; a close error is not actionable

	if _, err := file.Write(buf); err != nil {
		return fmt.Errorf("%w: write journal: %w", fault.ErrWriteFailure, err)
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("%w: sync journal: %w", fault.ErrWriteFailure, err)
	}

	return nil
}

type journalData struct {
	NewCap  uint64
	ValSize int
	Records []Record
}

// readJournal reads a grow journal: the target capacity, the value width and
// the records live when the grow was interrupted. Anything the bytes cannot
// be, a capacity above capLimit included, is an error, and the caller treats
// the journal as corrupt.
func readJournal(path string, capLimit uint64) (journalData, error) {
	const journalHeaderSize = 24

	data, err := xos.ReadFile(path)
	if err != nil {
		return journalData{}, fmt.Errorf("%w: read journal: %w", fault.ErrFilesystemFailure, err)
	}

	if len(data) < journalHeaderSize {
		return journalData{}, fmt.Errorf("%w: journal too small: %d bytes", fault.ErrInvalidArgument, len(data))
	}

	newCap := binary.BigEndian.Uint64(data[0:8])
	count := binary.BigEndian.Uint64(data[8:16])
	valSz := int(binary.BigEndian.Uint16(data[16:18]))

	if valSz == 0 {
		return journalData{}, fmt.Errorf("%w: journal value width is zero", fault.ErrInvalidArgument)
	}

	// The count is what the bytes hold, never the header's count times the
	// record width: that product wraps for a large enough count into a size
	// that matches, and the allocation it then asks for does not exist.
	journalRecordSize := keySize + valSz + tsSize
	body := len(data) - journalHeaderSize

	// #nosec G115 -- body is a length less the header, never negative
	if body%journalRecordSize != 0 || uint64(body/journalRecordSize) != count {
		return journalData{}, fmt.Errorf("%w: journal size mismatch: %d bytes for %d records of %d",
			fault.ErrInvalidArgument, body, count, journalRecordSize)
	}

	// The capacity holds the records under the load factor and is one a grow
	// could have written: under the index's own ceiling when it has one.
	if newCap == 0 || newCap > capLimit || float64(count)/float64(newCap) > maxLoadFactor {
		return journalData{}, fmt.Errorf("%w: journal capacity %d cannot hold %d records",
			fault.ErrInvalidArgument, newCap, count)
	}

	records := make([]Record, count)
	for idx := range records {
		off := journalHeaderSize + idx*journalRecordSize

		val := make([]byte, valSz)
		copy(val, data[off+keySize:off+keySize+valSz])

		records[idx] = Record{
			Key:   binary.BigEndian.Uint64(data[off : off+keySize]),
			Value: val,
			// #nosec G115 -- round-trip: uint64→int64 on read, int64→uint64 on write
			Timestamp: int64(binary.BigEndian.Uint64(data[off+keySize+valSz : off+keySize+valSz+tsSize])),
		}
	}

	return journalData{NewCap: newCap, ValSize: valSz, Records: records}, nil
}

// readJournalFor is the journal to replay for an index of valSize and maxCap,
// if there is one: none, or a corrupt one, which is deleted, is not found; a
// journal of another value width is an error.
func readJournalFor(journalPath string, valSize int, maxCap uint64) (journalData, bool, error) {
	_, err := xos.Stat(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return journalData{}, false, nil
	}

	if err != nil {
		return journalData{}, false, fmt.Errorf("%w: stat journal: %w", fault.ErrFilesystemFailure, err)
	}

	capLimit := maxJournalCap
	if maxCap != 0 && maxCap < capLimit {
		capLimit = maxCap
	}

	journal, err := readJournal(journalPath, capLimit)
	if err != nil {
		_ = os.Remove(journalPath)

		return journalData{}, false, nil //nolint:nilerr // a corrupt journal is dropped and the data file used as it is
	}

	if journal.ValSize != valSize {
		_ = os.Remove(journalPath)

		return journalData{}, false, fmt.Errorf("%w: journal valSize %d does not match expected %d",
			fault.ErrInvalidArgument, journal.ValSize, valSize)
	}

	return journal, true, nil
}

func (idx *Index) mmapDataFile() error {
	info, err := idx.dataFile.Stat()
	if err != nil {
		return fmt.Errorf("%w: stat for mmap: %w", fault.ErrFilesystemFailure, err)
	}

	size := info.Size()
	if size < headerSize {
		return fmt.Errorf("%w: file too small: %d bytes", fault.ErrInvalidArgument, size)
	}

	mapping, err := mmap.Map(idx.dataFile, int(size))
	if err != nil {
		return fmt.Errorf("%w: mmap: %w", fault.ErrFilesystemFailure, err)
	}

	idx.mapping = mapping
	idx.data = mapping.Bytes()

	return nil
}

func (idx *Index) offset(bucket uint64) int64 {
	// #nosec G115 -- bucket < capacity, which is bounded by int64-sized file
	return int64(headerSize) + int64(bucket)*idx.recordSize()
}

func (idx *Index) readHeader() header {
	return header{
		Magic:      binary.BigEndian.Uint32(idx.data[0:4]),
		Version:    binary.BigEndian.Uint32(idx.data[4:8]),
		ValSize:    binary.BigEndian.Uint16(idx.data[8:10]),
		Capacity:   binary.BigEndian.Uint64(idx.data[16:24]),
		Count:      binary.BigEndian.Uint64(idx.data[24:32]),
		Tombstones: binary.BigEndian.Uint64(idx.data[32:40]),
	}
}

func marshalHeader(dst []byte, hdr header) {
	binary.BigEndian.PutUint32(dst[0:4], hdr.Magic)
	binary.BigEndian.PutUint32(dst[4:8], hdr.Version)
	binary.BigEndian.PutUint16(dst[8:10], hdr.ValSize)
	binary.BigEndian.PutUint64(dst[16:24], hdr.Capacity)
	binary.BigEndian.PutUint64(dst[24:32], hdr.Count)
	binary.BigEndian.PutUint64(dst[32:40], hdr.Tombstones)
}

func marshalRecord(dst []byte, off int64, key uint64, value []byte, valSize int, timestamp int64) {
	vsz := int64(valSize)
	dst[off] = statusOccupied
	binary.BigEndian.PutUint64(dst[off+1:off+1+keySize], key)
	copy(dst[off+1+keySize:off+1+keySize+vsz], value)
	// #nosec G115 -- round-trip: int64→uint64 on write, uint64→int64 on read; bit pattern preserved
	binary.BigEndian.PutUint64(dst[off+1+keySize+vsz:off+1+keySize+vsz+tsSize], uint64(timestamp))
}

func (idx *Index) writeHeader(hdr header) {
	marshalHeader(idx.data, hdr)
}

func (idx *Index) readKey(off int64) uint64 {
	return binary.BigEndian.Uint64(idx.data[off+1 : off+1+keySize])
}

func (idx *Index) readRecord(off int64) Record {
	valOff := int64(idx.valSize)
	val := make([]byte, valOff)
	copy(val, idx.data[off+1+keySize:off+1+keySize+valOff])

	return Record{
		Key:   binary.BigEndian.Uint64(idx.data[off+1 : off+1+keySize]),
		Value: val,
		// #nosec G115 -- round-trip: int64→uint64 on write, uint64→int64 on read; bit pattern preserved
		Timestamp: int64(binary.BigEndian.Uint64(idx.data[off+1+keySize+valOff : off+1+keySize+valOff+tsSize])),
	}
}

func (idx *Index) writeRecord(off int64, key uint64, value []byte, timestamp int64) {
	marshalRecord(idx.data, off, key, value, idx.valSize, timestamp)
}

func (idx *Index) getUnlocked(key uint64) (Record, bool) {
	hdr := idx.readHeader()
	start := key % hdr.Capacity

	for probe := range hdr.Capacity {
		bucket := (start + probe) % hdr.Capacity
		off := idx.offset(bucket)
		status := idx.data[off]

		switch status {
		case statusEmpty:
			return Record{}, false
		case statusOccupied:
			if idx.readKey(off) == key {
				return idx.readRecord(off), true
			}
		default:
		}
	}

	return Record{}, false
}

// growLocked rebuilds the table without its tombstones, at double the
// capacity when the live records need it and at the same capacity otherwise.
//
//nolint:gocognit // the crash-safety protocol: its steps are ordered and read as one
func (idx *Index) growLocked() error {
	hdr := idx.readHeader()
	liveLoad := float64(hdr.Count+1) / float64(hdr.Capacity)

	// Tombstones trip the load limit as surely as live records, but the
	// rebuild drops them. Doubling for them grows the file without bound under
	// delete-and-insert churn, and at MaxCap fails a Put on a mostly empty
	// table.
	newCap := hdr.Capacity
	if liveLoad > maxLoadFactor/growFactor {
		newCap *= growFactor
	}

	if idx.maxCap != 0 && newCap > idx.maxCap {
		if liveLoad > maxLoadFactor {
			return fmt.Errorf("%w: grow to %d exceeds max %d", ErrCapacityExceeded, newCap, idx.maxCap)
		}

		newCap = hdr.Capacity
	}

	// Collect all live records.
	records := make([]Record, 0, hdr.Count)
	for bucket := range hdr.Capacity {
		off := idx.offset(bucket)
		if idx.data[off] == statusOccupied {
			records = append(records, idx.readRecord(off))
		}
	}

	// Take the setup lock to exclude a concurrent New and a peer's takeover
	// recovery, both of which look for the journal. Released after the
	// journal is deleted.
	setupLock, err := flock.Lock(idx.lockPath)
	if err != nil {
		return fmt.Errorf("%w: lock for grow: %w", fault.ErrFilesystemFailure, err)
	}

	// Unlock closes the lock file whatever happens, which releases the lock.
	defer func() { _ = flock.Unlock(setupLock) }()

	// Write journal before any data-file modification. If the process
	// crashes after this point, the next New, or a peer that takes the
	// lock, rebuilds the table from the journal.
	if err = writeJournal(idx.journalPath, newCap, idx.valSize, records); err != nil {
		_ = os.Remove(idx.journalPath)

		return err
	}

	// --- Begin in-place grow (crash-unsafe window, journal covers it) ---

	// Resize the file. The old mapping remains valid for its original range.
	// #nosec G115 -- same bound as create; capacity is bounded by available memory
	newSize := int64(headerSize) + int64(newCap)*idx.recordSize()
	if err = idx.dataFile.Truncate(newSize); err != nil {
		_ = os.Remove(idx.journalPath)

		return fmt.Errorf("%w: truncate: %w", fault.ErrWriteFailure, err)
	}

	// Map the grown file. On failure the old mapping is still intact.
	newMapping, err := mmap.Map(idx.dataFile, int(newSize))
	if err != nil {
		_ = os.Remove(idx.journalPath)

		return fmt.Errorf("%w: mmap new: %w", fault.ErrFilesystemFailure, err)
	}

	newData := newMapping.Bytes()

	// Zero the data region in the new mapping and write a fresh header.
	for i := range newData[headerSize:] {
		newData[headerSize+i] = 0
	}

	newHdr := header{
		Magic:   magic,
		Version: version,
		// #nosec G115 -- valSize is validated to be in [1, 65535] in New
		ValSize:  uint16(idx.valSize),
		Capacity: newCap,
	}
	marshalHeader(newData, newHdr)

	// Reinsert all records through the new mapping.
	recSize := idx.recordSize()

	for _, rec := range records {
		start := rec.Key % newCap
		for j := range newCap {
			bucket := (start + j) % newCap
			// #nosec G115 -- bucket < newCap, bounded by file size
			off := int64(headerSize) + int64(bucket)*recSize
			if newData[off] == statusEmpty {
				marshalRecord(newData, off, rec.Key, rec.Value, idx.valSize, rec.Timestamp)

				newHdr.Count++

				break
			}
		}
	}

	marshalHeader(newData, newHdr)

	// Swap: atomically transition idx from old mapping to new.
	oldMapping := idx.mapping
	idx.data = newData
	idx.mapping = newMapping
	idx.cap = newCap

	// Restore the write lock word in the new mapping.
	// #nosec G115 -- PIDs are always positive; 31-bit PID field cannot overflow
	atomic.StoreUint64(idx.lockWordPtr(), lockWriteFlag|(uint64(os.Getpid())<<lockPIDShift))

	// Unmap old — best-effort; leaked mappings are reclaimed on process exit.
	_ = oldMapping.Unmap()

	// --- End in-place grow ---

	// Flush to disk so the data file is durable before deleting the journal.
	if err := idx.mapping.Sync(); err != nil {
		return fmt.Errorf("%w: sync after grow: %w", fault.ErrWriteFailure, err)
	}

	_ = os.Remove(idx.journalPath)

	return nil
}

// reopenLocked remaps the underlying file at its current size. Called
// while holding the write lock (both in-process and cross-process) to
// pick up a capacity change made by another process's growLocked.
func (idx *Index) reopenLocked() error {
	// New the new fd first. On failure the old mapping is still intact.
	newFile, err := xos.OpenFile(idx.path, os.O_RDWR, filesystem.FilePermissionsPrivate)
	if err != nil {
		return fmt.Errorf("%w: reopen data file: %w", fault.ErrFilesystemFailure, err)
	}

	info, err := newFile.Stat()
	if err != nil {
		_ = newFile.Close()

		return fmt.Errorf("%w: stat for reopen: %w", fault.ErrFilesystemFailure, err)
	}

	newSize := info.Size()
	if newSize < headerSize {
		_ = newFile.Close()

		return fmt.Errorf("%w: file too small: %d bytes", fault.ErrInvalidArgument, newSize)
	}

	newMapping, err := mmap.Map(newFile, int(newSize))
	if err != nil {
		_ = newFile.Close()

		return fmt.Errorf("%w: mmap for reopen: %w", fault.ErrFilesystemFailure, err)
	}

	newData := newMapping.Bytes()

	// Restore write lock in the new mapping before swap.
	// #nosec G103 G115 -- PIDs are always positive; 31-bit PID field cannot overflow
	// #nosec G103 -- required for cross-process atomics on mmap'd region; offset 40 is 8-byte aligned
	atomic.StoreUint64((*uint64)(unsafe.Pointer(&newData[lockOffset])),
		lockWriteFlag|(uint64(os.Getpid())<<lockPIDShift))

	// Swap: save old state, install new.
	oldMapping, oldFile := idx.mapping, idx.dataFile
	idx.data = newData
	idx.mapping = newMapping
	idx.dataFile = newFile
	idx.cap = idx.readHeader().Capacity

	// Clean up old mapping and fd — best-effort.
	_ = oldMapping.Unmap()
	_ = oldFile.Close()

	return nil
}

// refresh takes the write lock, which recovers a dead writer's grow on the
// way, reopens the file if another process grew it, and lets go. The reader
// path goes through it when its lock could not be taken or its mapping is
// behind.
func (idx *Index) refresh() error {
	if err := idx.wlock(); err != nil {
		return err
	}

	defer idx.wunlock()

	if idx.readHeader().Capacity == idx.cap {
		return nil
	}

	return idx.reopenLocked()
}

// rlockFresh acquires a read lock, reopening the file first if another
// process has grown the table since this Index last mapped it, and recovering
// through the write lock what a dead writer left.
func (idx *Index) rlockFresh() error {
	for {
		if !idx.rlock() {
			if err := idx.refresh(); err != nil {
				return err
			}

			continue
		}

		if idx.readHeader().Capacity == idx.cap {
			return nil
		}

		idx.runlock()

		if err := idx.refresh(); err != nil {
			return err
		}
	}
}

// wlockFresh acquires a write lock, reopening the file first if another
// process has grown the table since this Index last mapped it.
func (idx *Index) wlockFresh() error {
	if err := idx.wlock(); err != nil {
		return err
	}

	if idx.readHeader().Capacity == idx.cap {
		return nil
	}

	if err := idx.reopenLocked(); err != nil {
		idx.wunlock()

		return err
	}

	return nil
}

// Locking: sync.RWMutex for in-process goroutine safety,
// atomic lock word in mmap'd header for cross-process safety.

func (idx *Index) lockWordPtr() *uint64 {
	// #nosec G103 -- required for cross-process atomics on mmap'd region; offset 40 is 8-byte aligned
	return (*uint64)(unsafe.Pointer(&idx.data[lockOffset]))
}

// readerSlotPtr returns a pointer to the PID slot at the given index in the
// mmap'd lock file. Each slot is a uint32.
func (idx *Index) readerSlotPtr(slot int) *uint32 {
	// #nosec G103 -- required for cross-process atomics on mmap'd lock file; slots are 4-byte aligned
	return (*uint32)(unsafe.Pointer(&idx.lockData[slot*4]))
}

// writerStartPtr returns a pointer to the writer's start time in the mmap'd
// lock file.
func (idx *Index) writerStartPtr() *uint64 {
	// #nosec G103 -- required for cross-process atomics on mmap'd lock file; the offset is 8-byte aligned
	return (*uint64)(unsafe.Pointer(&idx.lockData[writerStartOffset]))
}

// readerStartPtr returns a pointer to the start time recorded beside the PID
// slot at the given index.
func (idx *Index) readerStartPtr(slot int) *uint64 {
	// #nosec G103 -- required for cross-process atomics on mmap'd lock file; the offsets are 8-byte aligned
	return (*uint64)(unsafe.Pointer(&idx.lockData[readerStartsOffset+slot*8]))
}

// claimReaderSlot CAS-claims an empty PID slot in the lock file for this
// process, and records its start time beside it. Called on the per-process
// 0→1 reader transition.
func (idx *Index) claimReaderSlot() {
	// #nosec G115 -- PIDs are always positive and fit in uint32
	ourPID := uint32(os.Getpid())

	for slot := range maxReaderSlots {
		ptr := idx.readerSlotPtr(slot)
		if atomic.CompareAndSwapUint32(ptr, 0, ourPID) {
			atomic.StoreUint64(idx.readerStartPtr(slot), startOfSelf())
			idx.readerSlot.Store(int32(slot))

			return
		}
	}
	// All slots full — proceed without a slot. Stale-reader recovery
	// cannot detect this reader and may grant a writer the lock while
	// this reader is active. See maxReaderSlots.
}

// clearReaderSlot zeroes this process's PID slot in the lock file, the start
// time first so that no checker pairs the PID with another's start. Called on
// the per-process 1→0 reader transition and during Close.
func (idx *Index) clearReaderSlot() {
	slot := idx.readerSlot.Swap(-1)
	if slot >= 0 && idx.lockData != nil {
		atomic.StoreUint64(idx.readerStartPtr(int(slot)), 0)
		atomic.StoreUint32(idx.readerSlotPtr(int(slot)), 0)
	}
}

// allReaderPIDsDead reports whether every non-zero PID in the lock file
// reader slots belongs to a dead process, or to another process than the one
// that claimed the slot. Returns true when all slots are empty or all
// registered PIDs are dead.
func (idx *Index) allReaderPIDsDead() bool {
	for slot := range maxReaderSlots {
		slotPID := atomic.LoadUint32(idx.readerSlotPtr(slot))

		if slotPID != 0 && sameProcess(int(slotPID), atomic.LoadUint64(idx.readerStartPtr(slot))) {
			return false
		}
	}

	return true
}

// rlock takes the read lock, or reports false with nothing held when a dead
// writer holds the word: the write lock recovers what it left, so the caller
// goes through refresh and tries again.
//
//nolint:gocognit // the cross-process read-lock protocol, read top to bottom
func (idx *Index) rlock() bool {
	idx.mu.RLock()

	// The 0→1 transition claims a PID slot, so writers can tell this process
	// has readers.
	idx.slotMu.Lock()

	if idx.localReaders.Add(1) == 1 {
		idx.claimReaderSlot()
	}

	idx.slotMu.Unlock()

	ptr := idx.lockWordPtr()

	for spin := 0; ; spin++ {
		old := atomic.LoadUint64(ptr)

		if old&lockWriteFlag != 0 {
			// Writer is active. Check for stale writer before spinning further.
			if spin >= stalePIDThreshold {
				writerPID := int((old & lockPIDMask) >> lockPIDShift)
				if writerPID != 0 && !sameProcess(writerPID, atomic.LoadUint64(idx.writerStartPtr())) {
					idx.unclaim()
					idx.mu.RUnlock()

					return false
				}

				spin = 0

				continue
			}

			if spin < spinYieldCount {
				runtime.Gosched()
			} else {
				time.Sleep(time.Millisecond)
			}

			continue
		}

		if atomic.CompareAndSwapUint64(ptr, old, old+1) {
			return true
		}
	}
}

func (idx *Index) runlock() {
	atomic.AddUint64(idx.lockWordPtr(), ^uint64(0))
	idx.unclaim()
	idx.mu.RUnlock()
}

// unclaim is the per-process reader count going down one, the slot cleared
// at 1→0.
func (idx *Index) unclaim() {
	idx.slotMu.Lock()

	if idx.localReaders.Add(-1) == 0 {
		idx.clearReaderSlot()
	}

	idx.slotMu.Unlock()
}

// wlock takes the write lock. A dead writer's word becomes this process's,
// never free: a grow the writer died in has left a journal, which is
// replayed before anyone reads the table. A failure to replay it is
// returned with the dead writer's word put back, for the next taker.
//
//nolint:gocognit // the cross-process write-lock protocol, read top to bottom
func (idx *Index) wlock() error {
	idx.mu.Lock()

	ptr := idx.lockWordPtr()
	target := lockWriteFlag | (uint64(os.Getpid()) << lockPIDShift) // #nosec G115 -- PIDs are always positive; 31-bit PID field cannot overflow

	for spin := 0; ; spin++ {
		old := atomic.LoadUint64(ptr)

		if old == 0 {
			if atomic.CompareAndSwapUint64(ptr, 0, target) {
				atomic.StoreUint64(idx.writerStartPtr(), startOfSelf())

				return nil
			}

			continue
		}

		if spin >= stalePIDThreshold {
			spin = 0

			if old&lockWriteFlag != 0 {
				writerPID := int((old & lockPIDMask) >> lockPIDShift)
				if writerPID == 0 || sameProcess(writerPID, atomic.LoadUint64(idx.writerStartPtr())) {
					continue
				}

				// The start goes to zero before the word changes hands: a
				// checker that read our PID beside the dead writer's start
				// would take us for dead too. Zero is PID-only, the free
				// path's own window.
				deadStart := atomic.LoadUint64(idx.writerStartPtr())
				atomic.StoreUint64(idx.writerStartPtr(), 0)

				// Lost to another taker: the word is a live process's now, and
				// zero beside it is PID-only until its own store lands; the
				// dead start put back would read as its mismatch.
				if !atomic.CompareAndSwapUint64(ptr, old, target) {
					continue
				}

				atomic.StoreUint64(idx.writerStartPtr(), startOfSelf())

				if err := idx.recoverTakenOver(); err != nil {
					// Not free: the dead writer's word goes back, then its
					// start, so that the next checker takes over and tries
					// again, and no one reads the table it died in meanwhile.
					// Word first: with our PID still in it, the dead start
					// beside it would read as our mismatch.
					atomic.StoreUint64(ptr, old)
					atomic.StoreUint64(idx.writerStartPtr(), deadStart)
					idx.mu.Unlock()

					return err
				}

				return nil
			} else if idx.allReaderPIDsDead() {
				// All reader PIDs are dead (or no PIDs registered). The
				// reader count is stale — CAS directly to our write lock.
				if atomic.CompareAndSwapUint64(ptr, old, target) {
					atomic.StoreUint64(idx.writerStartPtr(), startOfSelf())

					return nil
				}
			}

			continue
		}

		if spin < spinYieldCount {
			runtime.Gosched()
		} else {
			time.Sleep(time.Millisecond)
		}
	}
}

// recoverTakenOver replays the journal a dead writer left, with its lock
// word now this process's and the setup lock held against New and a grow.
// A crash midway leaves the journal for the next taker, who starts over.
func (idx *Index) recoverTakenOver() error {
	setupLock, err := flock.Lock(idx.lockPath)
	if err != nil {
		return fmt.Errorf("%w: lock for recovery: %w", fault.ErrFilesystemFailure, err)
	}

	defer func() { _ = flock.Unlock(setupLock) }()

	journal, found, err := readJournalFor(idx.journalPath, idx.valSize, idx.maxCap)
	if err != nil || !found {
		return err
	}

	return idx.rebuild(journal)
}

// rebuild replays a journal into this process's mapping, the way the grow
// builds the table, in the file every live holder has mapped: the file is
// extended if the writer died before sizing it and never shrunk, the records
// go in, the header's counts last, and the lock word, inside every holder's
// mapping, is never written. The holders wait on it meanwhile and find the
// new capacity when it is theirs, as after any grow. The journal goes last,
// so a crash midway leaves it for the next rebuild, which starts over.
func (idx *Index) rebuild(journal journalData) error {
	// #nosec G115 -- NewCap is at most maxJournalCap, checked in readJournal
	size := int64(headerSize) + int64(journal.NewCap)*idx.recordSize()

	if err := idx.mapAtLeast(size); err != nil {
		return err
	}

	clear(idx.data[headerSize:size])

	recSize := idx.recordSize()

	var count uint64

	for _, rec := range journal.Records {
		start := rec.Key % journal.NewCap
		for probe := range journal.NewCap {
			off := int64(headerSize) + int64((start+probe)%journal.NewCap)*recSize // #nosec G115 -- bucket < NewCap
			if idx.data[off] == statusEmpty {
				marshalRecord(idx.data, off, rec.Key, rec.Value, idx.valSize, rec.Timestamp)

				count++

				break
			}
		}
	}

	idx.writeHeader(header{
		Magic:   magic,
		Version: version,
		// #nosec G115 -- valSize is validated to be in [1, 65535] in New
		ValSize:  uint16(idx.valSize),
		Capacity: journal.NewCap,
		Count:    count,
	})
	idx.cap = journal.NewCap

	if err := idx.mapping.Sync(); err != nil {
		return fmt.Errorf("%w: sync recovered table: %w", fault.ErrWriteFailure, err)
	}

	_ = os.Remove(idx.journalPath)

	return nil
}

// freeDeadWritersWord clears the lock word a rebuilt table keeps from the
// writer that died in the grow, so that the first operation after New does
// not wait out the stale threshold to take it over. Under the setup lock a
// journal belongs to a dead writer, unless a taker has already made the word
// its own: that taker is alive and keeps it.
func (idx *Index) freeDeadWritersWord() {
	word := atomic.LoadUint64(idx.lockWordPtr())
	if word&lockWriteFlag == 0 {
		return
	}

	writerPID := int((word & lockPIDMask) >> lockPIDShift)
	if writerPID == 0 || sameProcess(writerPID, atomic.LoadUint64(idx.writerStartPtr())) {
		return
	}

	// The start goes first, as on release: a word freed beside the dead
	// writer's start would pair that start with whoever takes the word next.
	// A taker that slips in before the start is zeroed is demoted to PID-only
	// liveness for its hold, and the CAS then fails on the moved word.
	atomic.StoreUint64(idx.writerStartPtr(), 0)
	atomic.CompareAndSwapUint64(idx.lockWordPtr(), word, 0)
}

// mapAtLeast makes the data file and this process's mapping of it at least
// size bytes, as a grow does: the file is extended, never shrunk, and a new
// mapping of the same file replaces the old, if any, the lock word in it
// unchanged.
func (idx *Index) mapAtLeast(size int64) error {
	info, err := idx.dataFile.Stat()
	if err != nil {
		return fmt.Errorf("%w: stat for recovery: %w", fault.ErrFilesystemFailure, err)
	}

	if info.Size() < size {
		if err = idx.dataFile.Truncate(size); err != nil {
			return fmt.Errorf("%w: extend for recovery: %w", fault.ErrWriteFailure, err)
		}
	} else {
		size = info.Size()
	}

	if idx.mapping != nil && int64(len(idx.data)) >= size {
		return nil
	}

	mapping, err := mmap.Map(idx.dataFile, int(size))
	if err != nil {
		return fmt.Errorf("%w: mmap for recovery: %w", fault.ErrFilesystemFailure, err)
	}

	if old := idx.mapping; old != nil {
		_ = old.Unmap()
	}

	idx.mapping, idx.data = mapping, mapping.Bytes()

	return nil
}

func (idx *Index) wunlock() {
	atomic.StoreUint64(idx.writerStartPtr(), 0)
	atomic.StoreUint64(idx.lockWordPtr(), 0)
	idx.mu.Unlock()
}
