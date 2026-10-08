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

// Package xos provides the same filesystem features as golang os package, with a key difference on windows
// (FILE_SHARE_DELETE). The one thing os does there that xos does not is the \\?\ prefix os adds to a long
// path on Windows 10 before 1703 and Server 2016; on anything later the Go runtime makes the process
// long-path aware and neither needs it.
package xos
