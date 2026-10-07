package app

import "testing"

func TestPlaybackHistorySourceIdentity(t *testing.T) {
	for _, input := range []string{"hongguo:7000000000000000001", "7000000000000000001"} {
		id, source, valid := playbackHistoryIdentity(input)
		if !valid || source != sourceHongguo || id != "hongguo:7000000000000000001" {
			t.Fatal(input, id, source, valid)
		}
	}
	for _, invalid := range []string{"", "huangdou:123", "huangguo:456", "huangguo-video:789", "legacy-123", "../data", "hongguo:bad"} {
		if _, _, valid := playbackHistoryIdentity(invalid); valid {
			t.Fatal("non-Hongguo history identity accepted", invalid)
		}
	}
}
