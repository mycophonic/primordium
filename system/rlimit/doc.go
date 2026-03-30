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

// Package rlimit raises the RLIMIT_NOFILE soft limit and ensures exec'd
// subprocesses inherit the raised value.  This is necessary because the
// Go runtime (1.19+) restores the original low limit before exec'ing
// children unless an explicit syscall.Setrlimit has been made.
package rlimit
