package gateway

type canonicalStopSequenceFilter struct {
	sequences      [][]rune
	sequenceValues []string
	maxLength      int
	pending        []rune
	template       canonicalStreamEvent
	matched        string
	stopped        bool
}

func newCanonicalStopSequenceFilter(values []string) *canonicalStopSequenceFilter {
	filter := &canonicalStopSequenceFilter{}
	seen := map[string]struct{}{}
	for _, value := range values {
		sequence := []rune(value)
		if len(sequence) == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		filter.sequences = append(filter.sequences, sequence)
		filter.sequenceValues = append(filter.sequenceValues, value)
		if len(sequence) > filter.maxLength {
			filter.maxLength = len(sequence)
		}
	}
	if len(filter.sequences) == 0 {
		return nil
	}
	return filter
}

func (f *canonicalStopSequenceFilter) Apply(events []canonicalStreamEvent) []canonicalStreamEvent {
	if f == nil || len(events) == 0 {
		return events
	}
	result := make([]canonicalStreamEvent, 0, len(events)+1)
	for _, event := range events {
		switch event.Type {
		case canonicalStreamEventTextDelta:
			if f.stopped || event.Delta == "" {
				continue
			}
			if len(f.pending) == 0 {
				f.template = event
			}
			f.pending = append(f.pending, []rune(event.Delta)...)
			matchIndex, sequenceIndex := f.firstMatch()
			if matchIndex >= 0 {
				result = append(result, f.emitPrefix(matchIndex)...)
				f.pending = nil
				f.matched = f.sequenceValues[sequenceIndex]
				f.stopped = true
				continue
			}
			keep := f.maxLength - 1
			if len(f.pending) > keep {
				result = append(result, f.emitPrefix(len(f.pending)-keep)...)
			}
		case canonicalStreamEventCreated:
			result = append(result, event)
		case canonicalStreamEventUsage:
			result = append(result, f.Flush()...)
			result = append(result, event)
		case canonicalStreamEventCompleted, canonicalStreamEventIncomplete, canonicalStreamEventError:
			result = append(result, f.Flush()...)
			if f.stopped {
				event.Type = canonicalStreamEventCompleted
				event.FinishReason = "stop_sequence"
				event.StopSequence = f.matched
				event.ErrorMessage = ""
			}
			result = append(result, event)
		default:
			if f.stopped {
				continue
			}
			result = append(result, f.Flush()...)
			result = append(result, event)
		}
	}
	return result
}

func (f *canonicalStopSequenceFilter) Flush() []canonicalStreamEvent {
	if f == nil || len(f.pending) == 0 || f.stopped {
		return nil
	}
	return f.emitPrefix(len(f.pending))
}

func (f *canonicalStopSequenceFilter) emitPrefix(length int) []canonicalStreamEvent {
	if length <= 0 || len(f.pending) == 0 {
		return nil
	}
	if length > len(f.pending) {
		length = len(f.pending)
	}
	event := f.template
	event.Delta = string(f.pending[:length])
	f.pending = append([]rune(nil), f.pending[length:]...)
	if len(f.pending) == 0 {
		f.template = canonicalStreamEvent{}
	}
	return []canonicalStreamEvent{event}
}

func (f *canonicalStopSequenceFilter) firstMatch() (int, int) {
	for index := 0; index < len(f.pending); index++ {
		for sequenceIndex, sequence := range f.sequences {
			if index+len(sequence) > len(f.pending) {
				continue
			}
			matched := true
			for offset := range sequence {
				if f.pending[index+offset] != sequence[offset] {
					matched = false
					break
				}
			}
			if matched {
				return index, sequenceIndex
			}
		}
	}
	return -1, -1
}
