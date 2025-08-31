package runc

import (
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type BundleRewriter interface {
	RewriteBundle(*specs.Spec) error
}

type ProcessRewriter interface {
	RewriteProcess(*specs.Process) error
}

type Wrapper struct {
	BundleRewriter  *BundleRewriter
	ProcessRewriter *ProcessRewriter
	Delegate        Cli
}

func (w *Wrapper) Run(cmd Command) error {
	// execute a delegated command the way that delegatec currently does, but:
	// 1. if BundleRewriter is defined, then upon create or start, RewriteBundle is
	//	  invoked with the contents of the config.json bundle and if changed they
	//    are flushed to disk after, but before actually delegating to Delegate
	// 2. if ProcessRewriter is defined then:
	//    2.1 when a process object exists as part of a bundle, RewriteProcess is
	//		  invoked on it, after RewriteBundle may have been called, but before
	//		  flushing to disk
	//    2.2 when a process.json exists, we read it, call RewriteProcess which modifies
	//        it and then flush to disk
	//    2.3 if exec is invoked via command line options then we construct a Process
	//        object, invoke RewriteProcess on it, store it as a process.json file and
	//        call Delegate's exec command but we pass it process.json instead of the
	//        initial arguments
	return nil
}
