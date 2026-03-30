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

	tempFile := filepath.Join(tempDir, objectKey)
	tempETag := filepath.Join(tempDir, objectKey+".etag")
	dataFile := filepath.Join(dataDir, objectKey)
	dataETag := filepath.Join(dataDir, objectKey+".etag")

	// Already complete in dataDir?
	if info, statErr := xos.Stat(dataFile); statErr == nil && info.Size() == remoteSize {
		if readETag(dataETag) == remoteETag {
			slog.Info("file already complete", "objectKey", objectKey, "size", remoteSize)

			return nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(tempFile), filesystem.DirPermissionsPrivate); err != nil {
		return fmt.Errorf("%w: %w", fault.ErrWriteFailure, err)
	}

	if err := os.MkdirAll(filepath.Dir(dataFile), filesystem.DirPermissionsPrivate); err != nil {
		return fmt.Errorf("%w: %w", fault.ErrWriteFailure, err)
	}

	// Check tempDir for a resumable partial download.
	var offset int64

	if info, statErr := xos.Stat(tempFile); statErr == nil {
		localETag := readETag(tempETag)

		if localETag != remoteETag {
			slog.Warn("remote object changed, discarding partial download",
				"local_etag", localETag, "remote_etag", remoteETag)

			_ = os.Remove(tempFile)
			_ = os.Remove(tempETag)
		} else {
			offset = info.Size()

			switch {
			case offset == remoteSize:
				slog.Info("temp file already complete, moving to data", "objectKey", objectKey)

				return moveToData(tempFile, tempETag, dataFile, dataETag)
			case offset > remoteSize:
				slog.Warn("local file larger than remote, re-downloading",
					"local", offset, "remote", remoteSize)

				_ = os.Remove(tempFile)
				_ = os.Remove(tempETag)

				offset = 0
			default:
			}
		}
	}

	// Write the ETag sidecar before starting a fresh download.
	if offset == 0 {
		if err := filesystem.WriteFile(tempETag, []byte(remoteETag), filesystem.FilePermissionsPrivate); err != nil {
			return fmt.Errorf("write etag: %w", err)
		}
	}

	if offset > 0 {
		slog.Info("resuming download", "objectKey", objectKey, "offset", offset, "total", remoteSize)
	} else {
		slog.Info("downloading", "objectKey", objectKey, "size", remoteSize)
	}

	expectedBytes := remoteSize - offset

	body, contentLength, err := cli.read(ctx, objectKey, offset)
	if err != nil {
		return err
	}

	defer body.Close()

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

	return moveToData(tempFile, tempETag, dataFile, dataETag)
}

func moveToData(tempFile, tempETag, dataFile, dataETag string) error {
	if err := os.Rename(tempFile, dataFile); err != nil {
		return fmt.Errorf("move data file: %w", err)
	}

	if err := os.Rename(tempETag, dataETag); err != nil {
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
	buf := make([]byte, 32<<10) //nolint:mnd // 32 KB buffer.

	var written int64

	nextLog := int64(progressBytes)

	for {
		if err := ctx.Err(); err != nil {
			return written, fmt.Errorf("cancelled: %w", err)
		}

		bytesRead, readErr := src.Read(buf)
		if bytesRead > 0 {
			bytesWritten, writeErr := dst.Write(buf[:bytesRead])
			if bytesWritten > 0 {
				written += int64(bytesWritten)
			}

			if writeErr != nil {
				return written, fmt.Errorf("write: %w", writeErr)
			}

			if bytesWritten != bytesRead {
				return written, fmt.Errorf(
					"%w: short write: %d of %d bytes",
					fault.ErrWriteFailure, bytesWritten, bytesRead,
				)
			}

			if written >= nextLog {
				slog.Info("download progress",
					"downloaded", offset+written,
					"total", total,
					"percent", (offset+written)*100/total, //nolint:mnd // Percentage.
				)

				nextLog += int64(progressBytes)
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}

			return written, fmt.Errorf("read: %w", readErr)
		}
	}

	return written, nil
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
