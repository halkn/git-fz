package git

import (
	"bytes"
	"fmt"
)

func splitNULGroups(data []byte, size int) ([][]string, error) {
	if size <= 0 {
		return nil, fmt.Errorf("invalid group size %d", size)
	}

	data = bytes.TrimSuffix(data, []byte{'\n'})
	fields := bytes.Split(data, []byte{0})
	if len(fields) > 0 && len(fields[len(fields)-1]) == 0 {
		fields = fields[:len(fields)-1]
	}
	if len(fields)%size != 0 {
		return nil, fmt.Errorf("expected groups of %d fields, got %d", size, len(fields))
	}

	groups := make([][]string, 0, len(fields)/size)
	for i := 0; i < len(fields); i += size {
		group := make([]string, size)
		for j := 0; j < size; j++ {
			field := fields[i+j]
			if j == 0 {
				field = bytes.TrimPrefix(field, []byte{'\n'})
			}
			group[j] = string(field)
		}
		groups = append(groups, group)
	}
	return groups, nil
}
