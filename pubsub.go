package torrentd

// func (td *Torrentd) subscribe() chan []byte {
// 	td.subsLock.Lock()
// 	defer td.subsLock.Unlock()

// 	c := make(chan []byte, 64)
// 	td.subs[c] = struct{}{}

// 	return c
// }

// func (td *Torrentd) unsubscribe(c chan []byte) {
// 	td.subsLock.Lock()
// 	defer td.subsLock.Unlock()

// 	delete(td.subs, c)
// 	close(c)
// }

// func (td *Torrentd) publish(b []byte) {
// 	td.subsLock.Lock()
// 	defer td.subsLock.Unlock()

// 	for c := range td.subs {
// 		select {
// 		case c <- b:
// 		default:
// 		}
// 	}
// }

// func (td *Torrentd) cancelSubscriptions() {
// 	td.subsLock.Lock()
// 	defer td.subsLock.Unlock()

// 	for c := range td.subs {
// 		close(c)
// 	}

// 	td.subs = nil
// }
