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

// Package compress is streaming zstd compression, registry-based
// decompression and tar archiving, over github.com/forkcloser/compress, with
// that module's five error classes mapped onto fault's: a stream no format
// claims is ErrNotImplemented, a bad stream or a failing source
// ErrReadFailure, a failing destination ErrWriteFailure, a call after Close
// ErrInvalidArgument.
//
// Zstd decompression is built in. Importing a format package registers its
// decoder with Decompress:
//
//	import _ "github.com/mycophonic/primordium/compress/gzip"
//	import _ "github.com/mycophonic/primordium/compress/xz"
//	import _ "github.com/mycophonic/primordium/compress/bzip2"
//	import _ "github.com/mycophonic/primordium/compress/lz4"
//
// A consumer that names its format, as one building an archive does, uses
// forkcloser/compress's format packages directly: their NewReader and
// NewWriter are the codecs.
package compress
