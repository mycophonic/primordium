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

// Package refcount provides primitives for a refcounted mechanism for shared folders
// that is safe to use across process.
// A typical use case is a shared folder where the provider can
// write temporary files on construction time, have consumers read it
// concurrently, then self-cleaning once readers are done.
// Note this is low-level:
// - creation factory is run on every acquire: it is the caller responsibility to decide what
// the factory should do if the resource has already been created
// - only the last `release` function is called: same thing, the consumer is responsible for dealing with that
// For a safer, higher-level API, use the volatile store instead.
package refcount
