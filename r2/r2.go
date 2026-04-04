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
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem/pathcheck"
)

// Config provides the consumer the ability to use specific credentials, endpoints and buckets.
type Config struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	Secret    string
	PathStyle bool
}

// Client is the main struct the consumer interacts with, providing all required methods for up and download.
type Client struct {
	under  *s3.Client
	bucket string
}

// New returns a new configured client.
func New(cfg *Config) (*Client, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("%w: endpoint must not be empty", fault.ErrInvalidArgument)
	}

	if cfg.Bucket == "" {
		return nil, fmt.Errorf("%w: bucket must not be empty", fault.ErrInvalidArgument)
	}

	if cfg.AccessKey == "" {
		return nil, fmt.Errorf("%w: access key must not be empty", fault.ErrInvalidArgument)
	}

	if cfg.Secret == "" {
		return nil, fmt.Errorf("%w: secret must not be empty", fault.ErrInvalidArgument)
	}

	return &Client{
		under: s3.New(s3.Options{
			Region:       "auto",
			BaseEndpoint: aws.String(cfg.Endpoint),
			UsePathStyle: cfg.PathStyle,
			Credentials: credentials.NewStaticCredentialsProvider(
				cfg.AccessKey,
				cfg.Secret,
				"",
			),
		}),
		bucket: cfg.Bucket,
	}, nil
}

// validateObjectKey checks that an S3 object key is safe to use as a
// filesystem path.  It splits on "/" (the S3 key separator) and validates
// each component via filesystem.ValidateComponent, which rejects
// empty segments, "..", null bytes, and platform-specific forbidden characters.
func validateObjectKey(objectKey string) error {
	if objectKey == "" {
		return fmt.Errorf("%w: object key must not be empty", fault.ErrInvalidArgument)
	}

	for component := range strings.SplitSeq(objectKey, "/") {
		if err := pathcheck.ValidateComponent(component); err != nil {
			return fmt.Errorf("%w: invalid object key %q: %w", fault.ErrInvalidArgument, objectKey, err)
		}
	}

	return nil
}

// ObjectInfo holds metadata returned by Stat.
type ObjectInfo struct {
	Size int64
	ETag string
}

// Stat returns metadata for the given object key.
// Returns fault.ErrNotFound if the object does not exist.
func (cli *Client) Stat(ctx context.Context, objectKey string) (*ObjectInfo, error) {
	resp, err := cli.under.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(cli.bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return nil, fmt.Errorf("%w: %s", fault.ErrNotFound, objectKey)
		}

		return nil, mapErr(err)
	}

	info := &ObjectInfo{}

	if resp.ContentLength != nil {
		info.Size = *resp.ContentLength
	}

	if resp.ETag != nil {
		info.ETag = strings.Trim(*resp.ETag, `"`)
	}

	return info, nil
}
