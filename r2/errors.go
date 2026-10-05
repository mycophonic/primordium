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
func mapErr(err error) error {
	// Context cancellation / deadline exceeded.
	if _, ok := errors.AsType[*smithy.CanceledError](err); ok {
		return fmt.Errorf("%w: %w", fault.ErrCancelled, err)
	}

	// TCP/DNS/TLS failure — request never reached the server.
	if _, ok := errors.AsType[*smithyhttp.RequestSendError](err); ok {
		return fmt.Errorf("%w: %w", fault.ErrNetworkCommunication, err)
	}

	// Not-found conditions (object, bucket, upload).
	if _, ok := errors.AsType[*types.NotFound](err); ok {
		return fmt.Errorf("%w: %w", fault.ErrNotFound, err)
	}

	if _, ok := errors.AsType[*types.NoSuchKey](err); ok {
		return fmt.Errorf("%w: %w", fault.ErrNotFound, err)
	}

	if _, ok := errors.AsType[*types.NoSuchBucket](err); ok {
		return fmt.Errorf("%w: %w", fault.ErrNotFound, err)
	}

	if _, ok := errors.AsType[*types.NoSuchUpload](err); ok {
		return fmt.Errorf("%w: %w", fault.ErrNotFound, err)
	}

	// Invalid SDK parameters (programming error).
	if _, ok := errors.AsType[*smithy.InvalidParamsError](err); ok {
		return fmt.Errorf("%w: %w", fault.ErrInvalidArgument, err)
	}

	// Serialization/deserialization failures (SDK or server protocol error).
	if _, ok := errors.AsType[*smithy.SerializationError](err); ok {
		return fmt.Errorf("%w: %w", fault.ErrSystemFailure, err)
	}

	if _, ok := errors.AsType[*smithy.DeserializationError](err); ok {
		return fmt.Errorf("%w: %w", fault.ErrSystemFailure, err)
	}

	// HTTP response errors — classify by status code.
	if respErr, ok := errors.AsType[*smithyhttp.ResponseError](err); ok {
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
