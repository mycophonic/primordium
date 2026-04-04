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

// Package iox provides buffered I/O wrappers that reduce syscall overhead
// for I/O-intensive workloads such as audio decoders and encoders.
//
// Small, frequent reads and writes are common in codec implementations —
// reading a few bytes of header, individual samples, or Huffman codes.
// Without buffering, each of these becomes a syscall, dominating CPU time.
// The wrappers in this package batch these operations into larger transfers,
// typically reducing thousands of syscalls to a handful per buffer fill.
//
// Three types are provided:
//
//   - [Reader] — buffered io.Reader (wraps bufio.Reader)
//   - [ReadSeeker] — buffered io.ReadSeeker with optimized small forward seeks
//   - [ReadWriter] — buffered io.ReadWriter with flush-on-close semantics
//
// All types optionally close the underlying source if it implements io.Closer.
package iox
