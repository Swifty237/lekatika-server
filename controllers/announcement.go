package controllers

// GetStrongestAnnouncement récupère la meilleure annonce parmi la liste
func GetStrongestAnnouncement(announcements []Announcement) *Announcement {
	if len(announcements) == 0 {
		return nil
	}
	best := &announcements[0]
	for i := 1; i < len(announcements); i++ {
		if CompareAnnouncements(announcements[i], *best) {
			best = &announcements[i]
		}
	}
	return best
}
