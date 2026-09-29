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

//revive:disable:add-constant
package r2

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/filesystem/xos"
)

// Download downloads an R2 object to dataDir/key with resume support.
// Temporary files are kept in tempDir/key during the download.
// On successful completion both the data file and its ETag sidecar
// are moved from tempDir to dataDir.
// Both directories must reside on the same filesystem (os.Rename is used).
func (cli *Client) Download(ctx context.Context, objectKey, tempDir, dataDir string) error {
	if err := validateObjectKey(objectKey); err != nil {
		return err
	}

	remoteInfo, err := cli.Stat(ctx, objectKey)
	if err != nil {
		return err
	}

	if remoteInfo.Size <= 0 {
		return fmt.Errorf("%w: remote object %q has no content", fault.ErrInvalidArgument, objectKey)
	}

	remoteSize := remoteInfo.Size
	remoteETag := remoteInfo.ETag

	paths := downloadPaths{
		tempFile: filepath.Join(tempDir, objectKey),
		tempETag: filepath.Join(tempDir, objectKey+".etag"),
		dataFile: filepath.Join(dataDir, objectKey),
		dataETag: filepath.Join(dataDir, objectKey+".etag"),
	}

	// Already complete in dataDir?
	if info, statErr := xos.Stat(paths.dataFile); statErr == nil && info.Size() == remoteSize {
		if readETag(paths.dataETag) == remoteETag {
			slog.InfoContext(ctx, "file already complete", "object_key", objectKey, "size", remoteSize)

			return nil
		}
	}

	if err = os.MkdirAll(filepath.Dir(paths.tempFile), filesystem.DirPermissionsPrivate); err != nil {
		return fmt.Errorf("%w: %w", fault.ErrWriteFailure, err)
	}

	if err = os.MkdirAll(filepath.Dir(paths.dataFile), filesystem.DirPermissionsPrivate); err != nil {
		return fmt.Errorf("%w: %w", fault.ErrWriteFailure, err)
	}

	offset, complete := resumeOffset(ctx, paths, remoteSize, remoteETag)
	if complete {
		slog.InfoContext(ctx, "temp file already complete, moving to data", "object_key", objectKey)

		return paths.moveToData()
	}

	if offset > 0 {
		slog.InfoContext(ctx, "resuming download", "object_key", objectKey, "offset", offset, "total", remoteSize)
	} else {
		// Write the ETag sidecar before starting a fresh download.
		err = filesystem.WriteFile(paths.tempETag, []byte(remoteETag), filesystem.FilePermissionsPrivate)
		if err != nil {
			return fmt.Errorf("write etag: %w", err)
		}

		slog.InfoContext(ctx, "downloading", "object_key", objectKey, "size", remoteSize)
	}

	if err = cli.fetchInto(ctx, objectKey, paths.tempFile, offset, remoteSize); err != nil {
		return err
	}

	return paths.moveToData()
}

// downloadPaths are a download's data file and ETag sidecar, in the temp
// directory while in progress and in the data directory once complete.
type downloadPaths struct {
	tempFile, tempETag, dataFile, dataETag string
}

// resumeOffset decides where a download starts from what the temp
// directory holds: at the end of a partial file of the same remote
// version, or at zero, discarding a partial file of another version or
// one larger than the remote object. complete reports a temp file that
// already holds the whole object.
func resumeOffset(
	ctx context.Context,
	paths downloadPaths,
	remoteSize int64,
	remoteETag string,
) (offset int64, complete bool) {
	info, statErr := xos.Stat(paths.tempFile)
	if statErr != nil {
		return 0, false
	}

	localETag := readETag(paths.tempETag)
	if localETag != remoteETag {
		slog.WarnContext(ctx, "remote object changed, discarding partial download",
			"local_etag", localETag, "remote_etag", remoteETag)

		_ = os.Remove(paths.tempFile)
		_ = os.Remove(paths.tempETag)

		return 0, false
	}

	offset = info.Size()

	switch {
	case offset == remoteSize:
		return offset, true
	case offset > remoteSize:
		slog.WarnContext(ctx, "local file larger than remote, re-downloading",
			"local", offset, "remote", remoteSize)

		_ = os.Remove(paths.tempFile)
		_ = os.Remove(paths.tempETag)

		return 0, false
	default:
		return offset, false
	}
}

// fetchInto reads the object from offset to its end into the temp file,
// appending to a partial one or truncating for a fresh download, then
// checks the file holds the whole object.
func (cli *Client) fetchInto(ctx context.Context, objectKey, tempFile string, offset, remoteSize int64) error {
	expectedBytes := remoteSize - offset

	body, contentLength, err := cli.read(ctx, objectKey, offset)
	if err != nil {
		return err
	}

	defer func() { _ = body.Close() }() // a response body: fully read or abandoned

	if contentLength > 0 && contentLength != expectedBytes {
		return fmt.Errorf("%w: server content-length %d, expected %d",
			fault.ErrUnacceptableResponse, contentLength, expectedBytes)
	}

	flags := os.O_WRONLY | os.O_CREATE
	if offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	file, err := xos.OpenFile(tempFile, flags, filesystem.FilePermissionsPrivate)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}

	written, err := copyWithProgress(ctx, file, body, offset, remoteSize)
	if closeErr := file.Close(); closeErr != nil && err == nil {
		err = fmt.Errorf("close: %w", closeErr)
	}

	if err != nil {
		return err
	}

	totalSize := offset + written
	if totalSize != remoteSize {
		return fmt.Errorf("%w: size mismatch: got %d, expected %d", fault.ErrReadFailure, totalSize, remoteSize)
	}

	return nil
}

func (paths downloadPaths) moveToData() error {
	if err := os.Rename(paths.tempFile, paths.dataFile); err != nil {
		return fmt.Errorf("move data file: %w", err)
	}

	if err := os.Rename(paths.tempETag, paths.dataETag); err != nil {
		return fmt.Errorf("move etag file: %w", err)
	}

	return nil
}

// readETag reads the ETag string from a sidecar file.
// Returns "" if the file does not exist or cannot be read.
func readETag(path string) string {
	data, err := xos.ReadFile(path)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

// copyWithProgress copies from src to dst, logging progress periodically.
func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, offset, total int64) (int64, error) {
	buf := make([]byte, copyBufferSize)

	var written int64

	nextLog := int64(progressBytes)

	for {
		if err := ctx.Err(); err != nil {
			return written, fmt.Errorf("cancelled: %w", err)
		}

		bytesRead, readErr := src.Read(buf)
		if bytesRead > 0 {
			bytesWritten, writeErr := writeChunk(dst, buf[:bytesRead])
			written += int64(bytesWritten)

			if writeErr != nil {
				return written, writeErr
			}

			if written >= nextLog {
				slog.InfoContext(ctx, "download progress",
					"downloaded", offset+written,
					"total", total,
					"percent", (offset+written)*100/total,
				)

				nextLog += int64(progressBytes)
			}
		}

		if readErr == io.EOF {
			break
		}

		if readErr != nil {
			return written, fmt.Errorf("read: %w", readErr)
		}
	}

	return written, nil
}

// writeChunk writes chunk to dst whole: a write error, or a short write the
// writer did not report, fails it. The count is what dst accepted.
func writeChunk(dst io.Writer, chunk []byte) (int, error) {
	bytesWritten, err := dst.Write(chunk)
	bytesWritten = max(bytesWritten, 0)

	if err != nil {
		return bytesWritten, fmt.Errorf("write: %w", err)
	}

	if bytesWritten != len(chunk) {
		return bytesWritten, fmt.Errorf(
			"%w: short write: %d of %d bytes",
			fault.ErrWriteFailure, bytesWritten, len(chunk),
		)
	}

	return bytesWritten, nil
}

// read returns a reader for the object starting at offset.
// When offset is 0 the full object is returned; when offset > 0 a range
// request is issued so that a partially-downloaded file can be resumed.
// The returned size is the number of bytes that will be delivered.
// The caller must close the returned ReadCloser.
func (cli *Client) read(ctx context.Context, objectKey string, offset int64) (io.ReadCloser, int64, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(cli.bucket),
		Key:    aws.String(objectKey),
	}

	if offset > 0 {
		input.Range = aws.String(fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := cli.under.GetObject(ctx, input)
	if err != nil {
		return nil, 0, mapErr(err)
	}

	var size int64
	if resp.ContentLength != nil {
		size = *resp.ContentLength
	}

	return resp.Body, size, nil
}
