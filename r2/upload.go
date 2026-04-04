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

package r2

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/filesystem/xos"
)

// MultipartOptions controls the behaviour of Upload.
type MultipartOptions struct {
	// PartSize is the number of bytes per part (default 100 MiB, minimum 5 MiB).
	// All parts except the last will be exactly this size.
	PartSize int64

	// StateDir is the directory where upload-state JSON files are persisted
	// so that an interrupted upload can be resumed.
	StateDir string

	// Concurrency is the number of parts uploaded in parallel.
	// Values ≤ 0 are treated as 1.
	Concurrency int
}

// uploadState is serialised to disk so that a crash-interrupted multipart
// upload can be resumed on the next invocation.
type uploadState struct {
	UploadID string          `json:"uploadId"`
	Key      string          `json:"key"`
	PartSize int64           `json:"partSize"`
	Total    int64           `json:"totalSize"`
	Parts    []completedPart `json:"parts"`
}

type completedPart struct {
	Number int32  `json:"number"`
	ETag   string `json:"etag"`
}

// Upload uploads content using S3 multipart upload with crash resumption.
// source must support random access (e.g. *os.File).
// totalSize is the full content length.  State is persisted to
// opts.StateDir as JSON so interrupted uploads can be resumed.
func (cli *Client) Upload(
	ctx context.Context,
	objectKey string,
	source io.ReaderAt,
	totalSize int64,
	opts MultipartOptions,
) error {
	if err := validateObjectKey(objectKey); err != nil {
		return err
	}

	if totalSize <= 0 {
		return fmt.Errorf("%w: totalSize must be positive", fault.ErrInvalidArgument)
	}

	opts = normaliseOptions(opts)

	numParts64 := (totalSize + opts.PartSize - 1) / opts.PartSize
	if numParts64 > maxParts {
		return fmt.Errorf("%w: %d (max %d)", fault.ErrInvalidArgument, numParts64, maxParts)
	}

	numParts := int32(numParts64) //nolint:gosec // bounds-checked above
	statePath := stateFilePath(opts.StateDir, cli.bucket, objectKey)

	state, err := cli.loadOrCreateUpload(ctx, objectKey, totalSize, opts, statePath)
	if err != nil {
		return err
	}

	if err := cli.uploadParts(ctx, objectKey, source, totalSize, opts, state, statePath, numParts); err != nil {
		return err
	}

	// Complete.
	if err := cli.completeUpload(ctx, objectKey, state); err != nil {
		return err
	}

	// Clean up state file.
	_ = os.Remove(statePath)

	return nil
}

// partWorker bundles the shared mutable state used by concurrent part uploads.
type partWorker struct {
	cli       *Client
	key       string
	source    io.ReaderAt
	state     *uploadState
	statePath string
	guard     sync.Mutex
	firstErr  error
}

// uploadParts uploads all remaining parts concurrently, persisting state after
// each successful part so that a crash-interrupted upload can resume.
func (cli *Client) uploadParts(
	ctx context.Context,
	objectKey string,
	source io.ReaderAt,
	totalSize int64,
	opts MultipartOptions,
	state *uploadState,
	statePath string,
	numParts int32,
) error {
	worker := &partWorker{
		cli:       cli,
		key:       objectKey,
		source:    source,
		state:     state,
		statePath: statePath,
	}

	done := completedSet(state.Parts)
	semaphore := make(chan struct{}, opts.Concurrency)

	var workerGroup sync.WaitGroup

	for partNum := int32(1); partNum <= numParts; partNum++ {
		if done[partNum] {
			continue
		}

		if worker.shouldStop(ctx) {
			break
		}

		offset := int64(partNum-1) * opts.PartSize
		partLen := min(opts.PartSize, totalSize-offset)

		semaphore <- struct{}{} // acquire slot

		workerGroup.Add(1)

		go func(pNum int32, pOffset, pLen int64) {
			defer workerGroup.Done()
			defer func() { <-semaphore }() // release slot

			worker.uploadOne(ctx, pNum, pOffset, pLen)
		}(partNum, offset, partLen)
	}

	workerGroup.Wait()

	return worker.firstErr
}

func (pw *partWorker) uploadOne(ctx context.Context, partNumber int32, partOffset, partLen int64) {
	reader := io.NewSectionReader(pw.source, partOffset, partLen)

	etag, uploadErr := pw.cli.uploadPart(ctx, pw.key, pw.state.UploadID, partNumber, reader, partLen)
	if uploadErr != nil {
		pw.setErr(fmt.Errorf("part %d: %w", partNumber, uploadErr))

		return
	}

	pw.guard.Lock()

	pw.state.Parts = append(pw.state.Parts, completedPart{Number: partNumber, ETag: etag})
	saveErr := saveState(pw.statePath, pw.state)

	pw.guard.Unlock()

	if saveErr != nil {
		pw.setErr(fmt.Errorf("save state: %w", saveErr))
	}
}

func (pw *partWorker) shouldStop(ctx context.Context) bool {
	pw.guard.Lock()

	failed := pw.firstErr != nil

	pw.guard.Unlock()

	if failed {
		return true
	}

	if err := ctx.Err(); err != nil {
		pw.setErr(err)

		return true
	}

	return false
}

func (pw *partWorker) setErr(err error) {
	pw.guard.Lock()
	defer pw.guard.Unlock()

	if pw.firstErr == nil {
		pw.firstErr = err
	}
}

// loadOrCreateUpload either resumes from a persisted state file or starts a
// new multipart upload.
func (cli *Client) loadOrCreateUpload(
	ctx context.Context,
	objectKey string,
	totalSize int64,
	opts MultipartOptions,
	statePath string,
) (*uploadState, error) {
	state, err := loadState(statePath)
	if err == nil {
		// If part size or total size changed, the old upload's part boundaries
		// no longer match — abort and start fresh.
		if state.PartSize != opts.PartSize || state.Total != totalSize {
			cli.abortUpload(ctx, objectKey, state.UploadID)

			_ = os.Remove(statePath)

			return cli.createUpload(ctx, objectKey, totalSize, opts, statePath)
		}

		// Verify the upload still exists on R2.
		parts, listErr := cli.listParts(ctx, objectKey, state.UploadID)
		if listErr != nil {
			// Upload expired or was aborted — start fresh.
			cli.abortUpload(ctx, objectKey, state.UploadID)

			_ = os.Remove(statePath)

			return cli.createUpload(ctx, objectKey, totalSize, opts, statePath)
		}

		// Reconcile: trust R2 as source of truth.
		state.Parts = parts

		if err := saveState(statePath, state); err != nil {
			return nil, fmt.Errorf("save reconciled state: %w", err)
		}

		return state, nil
	}

	return cli.createUpload(ctx, objectKey, totalSize, opts, statePath)
}

func (cli *Client) createUpload(
	ctx context.Context,
	objectKey string,
	totalSize int64,
	opts MultipartOptions,
	statePath string,
) (*uploadState, error) {
	resp, err := cli.under.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(cli.bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return nil, mapErr(err)
	}

	state := &uploadState{
		UploadID: *resp.UploadId,
		Key:      objectKey,
		PartSize: opts.PartSize,
		Total:    totalSize,
	}

	if err := saveState(statePath, state); err != nil {
		cli.abortUpload(ctx, objectKey, *resp.UploadId)

		return nil, fmt.Errorf("save initial state: %w", err)
	}

	return state, nil
}

// abortUpload issues a best-effort AbortMultipartUpload. Errors are discarded
// because the caller is already handling a failure or switching to a new upload.
func (cli *Client) abortUpload(ctx context.Context, objectKey, uploadID string) {
	_, _ = cli.under.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(cli.bucket),
		Key:      aws.String(objectKey),
		UploadId: aws.String(uploadID),
	})
}

func (cli *Client) uploadPart(
	ctx context.Context,
	objectKey string,
	uploadID string,
	partNumber int32,
	body io.Reader,
	contentLength int64,
) (string, error) {
	resp, err := cli.under.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:        aws.String(cli.bucket),
		Key:           aws.String(objectKey),
		UploadId:      aws.String(uploadID),
		PartNumber:    aws.Int32(partNumber),
		Body:          body,
		ContentLength: aws.Int64(contentLength),
	})
	if err != nil {
		return "", mapErr(err)
	}

	if resp.ETag == nil {
		return "", fmt.Errorf("%w: UploadPart response missing ETag", fault.ErrUnacceptableResponse)
	}

	return *resp.ETag, nil
}

func (cli *Client) completeUpload(ctx context.Context, objectKey string, state *uploadState) error {
	// Parts must be sorted by number.
	slices.SortFunc(state.Parts, func(a, b completedPart) int {
		return cmp.Compare(a.Number, b.Number)
	})

	s3Parts := make([]types.CompletedPart, len(state.Parts))
	for idx := range state.Parts {
		s3Parts[idx] = types.CompletedPart{
			PartNumber: aws.Int32(state.Parts[idx].Number),
			ETag:       aws.String(state.Parts[idx].ETag),
		}
	}

	_, err := cli.under.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(cli.bucket),
		Key:      aws.String(objectKey),
		UploadId: aws.String(state.UploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: s3Parts,
		},
	})
	if err != nil {
		return mapErr(err)
	}

	return nil
}

func (cli *Client) listParts(ctx context.Context, key, uploadID string) ([]completedPart, error) {
	var result []completedPart

	var marker *string

	for {
		input := &s3.ListPartsInput{
			Bucket:   aws.String(cli.bucket),
			Key:      aws.String(key),
			UploadId: aws.String(uploadID),
			MaxParts: aws.Int32(listPartsMaxKeys),
		}

		if marker != nil {
			input.PartNumberMarker = marker
		}

		resp, err := cli.under.ListParts(ctx, input)
		if err != nil {
			var noUpload *types.NoSuchUpload
			if errors.As(err, &noUpload) {
				return nil, fmt.Errorf("upload %q expired or aborted: %w", uploadID, err)
			}

			return nil, mapErr(err)
		}

		for idx := range resp.Parts {
			part := resp.Parts[idx]
			if part.PartNumber != nil && part.ETag != nil {
				result = append(result, completedPart{
					Number: *part.PartNumber,
					ETag:   *part.ETag,
				})
			}
		}

		if resp.IsTruncated == nil || !*resp.IsTruncated {
			break
		}

		marker = resp.NextPartNumberMarker
	}

	return result, nil
}

// --- state persistence ---

func stateFilePath(stateDir, bucket, objectKey string) string {
	hash := sha256.Sum256([]byte(bucket + "/" + objectKey))

	return filepath.Join(stateDir, hex.EncodeToString(hash[:16])+".upload.json")
}

func loadState(statePath string) (*uploadState, error) {
	data, err := xos.ReadFile(statePath)
	if err != nil {
		return nil, fmt.Errorf("read upload state: %w", err)
	}

	var state uploadState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode upload state: %w", err)
	}

	return &state, nil
}

func saveState(statePath string, state *uploadState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode upload state: %w", err)
	}

	tmpPath := statePath + ".tmp"

	if err := filesystem.WriteFile(tmpPath, data, stateFileMode); err != nil {
		return fmt.Errorf("write temp state: %w", err)
	}

	if err := os.Rename(tmpPath, statePath); err != nil {
		return fmt.Errorf("rename state: %w", err)
	}

	return nil
}

func completedSet(parts []completedPart) map[int32]bool {
	done := make(map[int32]bool, len(parts))
	for _, part := range parts {
		done[part.Number] = true
	}

	return done
}

func normaliseOptions(opts MultipartOptions) MultipartOptions {
	if opts.PartSize < minPartSize {
		opts.PartSize = minPartSize
	}

	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}

	return opts
}
