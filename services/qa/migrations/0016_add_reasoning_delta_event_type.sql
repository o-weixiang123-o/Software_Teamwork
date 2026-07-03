-- +goose Up
ALTER TABLE response_stream_events
    DROP CONSTRAINT IF EXISTS response_stream_events_event_type_check;

ALTER TABLE response_stream_events
    ADD CONSTRAINT response_stream_events_event_type_check CHECK (
        event_type IN (
            'message.created', 'agent.iteration.started', 'reasoning.step',
            'reasoning.delta', 'tool.started', 'tool.completed', 'tool.failed',
            'answer.delta', 'citation.delta', 'answer.completed', 'error', 'heartbeat'
        )
    );

-- +goose Down
ALTER TABLE response_stream_events
    DROP CONSTRAINT IF EXISTS response_stream_events_event_type_check;

ALTER TABLE response_stream_events
    ADD CONSTRAINT response_stream_events_event_type_check CHECK (
        event_type IN (
            'message.created', 'agent.iteration.started', 'reasoning.step',
            'tool.started', 'tool.completed', 'tool.failed', 'answer.delta',
            'citation.delta', 'answer.completed', 'error', 'heartbeat'
        )
    );
