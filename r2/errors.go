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
	"errors"
	"fmt"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/mycophonic/primordium/fault"
)

// mapErr classifies an AWS SDK error into an appropriate fault sentinel.
//
//nolint:cyclop // Flat type-switch; each branch is trivial.
func mapErr(err error) error {
	// Context cancellation / deadline exceeded.
	var cancelErr *smithy.CanceledError
	if errors.As(err, &cancelErr) {
		return fmt.Errorf("%w: %w", fault.ErrCancelled, err)
	}

	// TCP/DNS/TLS failure — request never reached the server.
	var sendErr *smithyhttp.RequestSendError
	if errors.As(err, &sendErr) {
		return fmt.Errorf("%w: %w", fault.ErrNetworkCommunication, err)
	}

	// Not-found conditions (object, bucket, upload).
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return fmt.Errorf("%w: %w", fault.ErrNotFound, err)
	}

	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return fmt.Errorf("%w: %w", fault.ErrNotFound, err)
	}

	var noSuchBucket *types.NoSuchBucket
	if errors.As(err, &noSuchBucket) {
		return fmt.Errorf("%w: %w", fault.ErrNotFound, err)
	}

	var noSuchUpload *types.NoSuchUpload
	if errors.As(err, &noSuchUpload) {
		return fmt.Errorf("%w: %w", fault.ErrNotFound, err)
	}

	// Invalid SDK parameters (programming error).
	var invalidParams *smithy.InvalidParamsError
	if errors.As(err, &invalidParams) {
		return fmt.Errorf("%w: %w", fault.ErrInvalidArgument, err)
	}

	// Serialization/deserialization failures (SDK or server protocol error).
	var serErr *smithy.SerializationError
	if errors.As(err, &serErr) {
		return fmt.Errorf("%w: %w", fault.ErrSystemFailure, err)
	}

	var deserErr *smithy.DeserializationError
	if errors.As(err, &deserErr) {
		return fmt.Errorf("%w: %w", fault.ErrSystemFailure, err)
	}

	// HTTP response errors — classify by status code.
	var respErr *smithyhttp.ResponseError
	if errors.As(err, &respErr) {
		return mapHTTPStatus(respErr.HTTPStatusCode(), err)
	}

	// Unrecognized error structure.
	return fmt.Errorf("%w: %w", fault.ErrSystemFailure, err)
}

// mapHTTPStatus maps an HTTP status code to the appropriate fault sentinel.
func mapHTTPStatus(code int, err error) error {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: %w", fault.ErrAuthenticationFailure, err)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %w", fault.ErrNotFound, err)
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return fmt.Errorf("%w: %w", fault.ErrTimeout, err)
	default:
		return fmt.Errorf("%w: %w", fault.ErrUnacceptableResponse, err)
	}
}
