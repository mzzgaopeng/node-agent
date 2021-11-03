package underscore

import "reflect"

func (m *query) Take(count int) IQuery {
	index := 0
	return m.Where(func(_, _ interface{}) bool {
		index = index + 1
		return index <= count
	})
}

func (m enumerable) Take(count int) IEnumerable {
	return enumerable{
		Enumerator: func() IEnumerator {
			iterator := m.GetEnumerator()
			return &enumerator{
				MoveNextFunc: func() (valueRV reflect.Value, keyRV reflect.Value, ok bool) {
					if count <= 0 {
						return
					}

					count--
					if ok = iterator.MoveNext(); ok {
						valueRV = iterator.GetValue()
						keyRV = iterator.GetKey()
					}

					return
				},
			}
		},
	}
}
