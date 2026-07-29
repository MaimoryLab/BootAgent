package app

// The write lock serialises the operations that read a config, merge into it and
// write it back.
//
// This is a new concurrency surface the desktop shell brings rather than one the
// HTTP server had: http.server handled one request at a time, while every Wails
// binding call runs on its own goroutine. ADR-008 records the requirement; this is
// it.
//
// The hazard is not a torn file. Each write goes to its own temporary and is
// published with an atomic rename, so a reader always sees one whole version. It is
// lost work: Install and Activate read the user's existing config, merge the managed
// fields into it and write the result, so two of them interleaved means the second
// read happens before the first write and the first Agent's changes are discarded by
// last-writer-wins. Widening the window between read and write turns that from
// theoretical into three of four merges lost, which is how the need for this was
// confirmed rather than assumed.
//
// A field on Service rather than a package-level mutex: the lock belongs to the home
// being written, and two Services over different homes have no reason to block each
// other. In production there is one Service, so this serialises everything that
// matters.
//
// One lock rather than one per file, because these operations also write the shared
// env file and the profile pointer -- per-file locking would need a lock ordering to
// avoid deadlock, for operations that are user-initiated and rare. Contention is not
// a concern here; losing a user's config edits is.
//
// Reads are deliberately not serialised. Status only reads, and the atomic rename
// means it cannot observe a half-written file, so taking the lock there would let an
// install block the overview from rendering for no gain.
func (s *Service) lockWrites() func() {
	s.writes.Lock()
	return s.writes.Unlock
}
