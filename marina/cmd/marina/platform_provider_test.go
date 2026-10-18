package main

import (
	"github.com/jmlero/the-platform/marina/internal/flow"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/reconcile"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
)

// Marina's own ledger serves every consumer the remote provider served.
var (
	_ reconcile.ControlProvider                 = (*sqlite.Platform)(nil)
	_ reconcile.LeaseProvider                   = (*sqlite.Platform)(nil)
	_ flow.Provider                             = (*sqlite.Platform)(nil)
	_ flow.RepositoryProvider                   = (*sqlite.Platform)(nil)
	_ workcontrol.LandedCompletion              = (*sqlite.Platform)(nil)
	_ workcontrol.QuestionProjection            = (*sqlite.Platform)(nil)
	_ workcontrol.ContextInspection             = (*sqlite.Platform)(nil)
	_ workcontrol.WorkloadPublication           = (*sqlite.Platform)(nil)
	_ workcontrol.ConversationSource            = (*sqlite.Platform)(nil)
	_ workcontrol.ConversationContextInspection = (*sqlite.Platform)(nil)
	_ workcontrol.ProviderInformation           = (*sqlite.Platform)(nil)
	_ workcontrol.AgentRegistry                 = (*sqlite.Platform)(nil)
)
