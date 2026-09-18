package runtime

import "context"

// Controller applies an optional Hook Pipeline around Run mutations. It is the
// in-process execution boundary used by the existing single-agent adapter.
type Controller struct {
	Run   *Run
	Hooks *Pipeline
}

func (c *Controller) Transition(ctx context.Context, next RunStatus) error {
	return c.execute(ctx, HookBeforeTransition, nil, nil, func() error { return c.Run.Transition(next) })
}

func (c *Controller) SubmitAction(ctx context.Context, action Action) error {
	return c.execute(ctx, HookBeforeAction, &action, nil, func() error { return c.Run.SubmitAction(action) })
}

func (c *Controller) Observe(ctx context.Context, observation Observation) error {
	return c.execute(ctx, HookBeforeObservation, nil, &observation, func() error { return c.Run.Observe(observation) })
}

func (c *Controller) Complete(ctx context.Context) error {
	return c.execute(ctx, HookBeforeTransition, nil, nil, c.Run.Complete)
}

func (c *Controller) execute(ctx context.Context, stage HookStage, action *Action, observation *Observation, next func() error) error {
	if c.Hooks == nil {
		return next()
	}
	event := HookEvent{Stage: stage, Run: c.Run.Snapshot(), Action: action, Observation: observation}
	return c.Hooks.Execute(ctx, event, func(context.Context) error { return next() })
}
