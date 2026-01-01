package fswatch

// OpString returns a string label for an Op.
func OpString(op Op) string {
	switch op {
	case OpCreate:
		return "CREATE"
	case OpWrite:
		return "WRITE"
	case OpRemove:
		return "REMOVE"
	case OpRename:
		return "RENAME"
	case OpChmod:
		return "CHMOD"
	default:
		return "UNKNOWN"
	}
}

func mergeOp(current, next Op) Op {
	priority := map[Op]int{
		OpRemove:  5,
		OpRename:  4,
		OpCreate:  3,
		OpWrite:   2,
		OpChmod:   1,
		OpUnknown: 0,
	}
	if priority[next] >= priority[current] {
		return next
	}
	return current
}
