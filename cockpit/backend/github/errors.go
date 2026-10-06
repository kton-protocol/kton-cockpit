package github

import (
	"errors"

	"github.com/kton-protocol/kton-cockpit/internal/gitops"
)

func asPushFailed(err error, pf **gitops.PushFailed) bool { return errors.As(err, pf) }
