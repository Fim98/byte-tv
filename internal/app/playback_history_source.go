package app

func playbackHistoryIdentity(id string) (string, string, bool) {
	source, sourceID, valid := splitProviderDramaID(id)
	if !valid || source != sourceHongguo {
		return "", "", false
	}
	return providerDramaID(source, sourceID), source, true
}
