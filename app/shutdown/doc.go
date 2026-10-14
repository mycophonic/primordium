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

// Package shutdown runs registered cleanup handlers, in reverse order of
// registration and once, when the process ends through [Run] or [Exit], a
// panic in a goroutine started with [Go], or a shutdown signal.
//
// Nothing runs them on a plain return from main: a program ends through [Run],
// or through [Exit] where a command library exits by itself. A panic in a
// goroutine started with a bare go statement crashes the process without
// them. SIGKILL cannot be caught.
package shutdown
