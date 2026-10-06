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

// Package pathcheck provides utilities to enforce platform specific path validation.
//
// A path is valid when every component is a name a filesystem entry can carry
// on a given Platform: Linux, Darwin or Windows. Every platform's rules are
// available everywhere, so a path can be checked for Linux from macOS; the
// package-level functions check for Native, the platform the program runs on.
// "." and ".." are not names, so a path containing them is refused: pathcheck
// neither cleans nor resolves a path.
//
// To check a path a user typed, which may be relative, validate its absolute
// form: filepath.Abs cleans it, leaving no "." or ".." component. filepath
// cleans for Native only; for another POSIX platform, path.Clean does the
// same, and the standard library has no cleaner for Windows paths elsewhere.
// Cleaning resolves ".." lexically, so "../secret" validates as the absolute
// path it names: pathcheck says nothing about where a path points. Keeping a
// path under a directory is os.Root's job, or filepath.IsLocal's.
package pathcheck
