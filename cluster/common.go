package cluster

const CIRCBUFMAPLEN = 100

// CircBufMap is a map of arrays that is kept at a fixed length. When
// at full capacity and another item is added, the oldest item drops
// off.

type CircBufMap map[string][]interface{}

func NewCircBufMap() CircBufMap {
	return make(CircBufMap)
}

// Last returns the most recent item added.
func (c CircBufMap) Last(node string) interface{} {
	return c[node][len(c[node])-1]
}

// MakeNode assigns a new empty slice to the map entry for this node
func (c CircBufMap) MakeNode(node string) {
	c[node] = make([]interface{}, 0)
}

// Append adds a new item to the array in the map indexed by the
// node. If the array is already full, discard the oldest item.
func (c CircBufMap) Append(node string, item interface{}) {
	if _, ok := c[node]; !ok {
		c.MakeNode(node)
	}
	if len(c[node]) >= CIRCBUFMAPLEN {
		c[node] = c[node][1:]
	}
	c[node] = append(c[node], item)
}
