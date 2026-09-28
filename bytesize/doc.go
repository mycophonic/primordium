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

// Package bytesize names the multiples of a byte an int64 can count: the
// decimal (SI) ones, powers of 1000 from KB to EB, and the binary (IEC) ones,
// powers of 1024 from KiB to EiB. The constants are untyped, so they combine
// with any integer or float type: 5 * bytesize.MiB.
package bytesize
