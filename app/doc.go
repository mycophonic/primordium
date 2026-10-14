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

// Package app sets a process up the way every mycophonic program is, the
// filesystem defaults and locations under its name, the logger, the network
// defaults, the crash reporter, the file limit, and the shutdown handlers,
// and runs it through Main, so that the handlers run however it ends short
// of a kill.
package app
