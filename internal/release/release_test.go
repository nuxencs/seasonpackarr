package release

import (
	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/stretchr/testify/assert"
	"testing"
)

func Test_MatchEpToSeasonPackEp(t *testing.T) {
	type args struct {
		clientEpPath  string
		clientEpSize  int64
		torrentEpPath string
		torrentEpSize int64
	}

	type compare struct {
		path string
		info domain.CompareInfo
	}

	tests := []struct {
		name string
		args args
		want compare
	}{
		{
			name: "found_match",
			args: args{
				clientEpPath:  "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S02E01 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "Series Title 2022 S02E01 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				info: domain.CompareInfo{},
			},
		},
		{
			name: "wrong_episode",
			args: args{
				clientEpPath:  "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S02E02 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "",
				info: domain.CompareInfo{
					StatusCode:         domain.StatusEpisodeMismatch,
					ClientRejectField:  1,
					TorrentRejectField: 2,
				},
			},
		},
		{
			name: "wrong_season",
			args: args{
				clientEpPath:  "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S03E01 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "",
				info: domain.CompareInfo{
					StatusCode:         domain.StatusSeasonMismatch,
					ClientRejectField:  2,
					TorrentRejectField: 3,
				},
			},
		},
		{
			name: "wrong_resolution",
			args: args{
				clientEpPath:  "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S02E01 2160p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "",
				info: domain.CompareInfo{
					StatusCode:         domain.StatusResolutionMismatch,
					ClientRejectField:  "1080p",
					TorrentRejectField: "2160p",
				},
			},
		},
		{
			name: "wrong_rlsgrp",
			args: args{
				clientEpPath:  "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S02E01 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-OtherRlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "",
				info: domain.CompareInfo{
					StatusCode:         domain.StatusRlsGrpMismatch,
					ClientRejectField:  "RlsGrp",
					TorrentRejectField: "OtherRlsGrp",
				},
			},
		},
		{
			name: "wrong_size",
			args: args{
				clientEpPath:  "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S02E01 1080p Test ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2278773077,
			},
			want: compare{
				path: "",
				info: domain.CompareInfo{
					StatusCode:         domain.StatusSizeMismatch,
					ClientRejectField:  int64(2316560346),
					TorrentRejectField: int64(2278773077),
				},
			},
		},
		{
			name: "subfolder_in_client",
			args: args{
				clientEpPath:  "Test/Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				info: domain.CompareInfo{},
			},
		},
		{
			name: "subfolder_in_torrent",
			args: args{
				clientEpPath:  "Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Test/Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "Test/Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				info: domain.CompareInfo{},
			},
		},
		{
			name: "subfolder_in_both",
			args: args{
				clientEpPath:  "Test/Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Test/Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "Test/Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				info: domain.CompareInfo{},
			},
		},
		{
			name: "multi_subfolder",
			args: args{
				clientEpPath:  "/data/torrents/tv/Test/Series Title 2022 S02E01 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				clientEpSize:  2316560346,
				torrentEpPath: "Series Title 2022 S02/Test/Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				torrentEpSize: 2316560346,
			},
			want: compare{
				path: "Series Title 2022 S02/Test/Series Title 2022 S02E01 Test 1080p ATVP WEB-DL DDP 5.1 Atmos H.264-RlsGrp.mkv",
				info: domain.CompareInfo{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotInfo := MatchEpToSeasonPackEp(tt.args.clientEpPath, tt.args.clientEpSize, tt.args.torrentEpPath, tt.args.torrentEpSize)

			got := compare{
				path: gotPath,
				info: gotInfo,
			}

			assert.Equalf(t, tt.want, got, "MatchEpToSeasonPackEp(%v, %v, %v, %v)",
				tt.args.clientEpPath, tt.args.clientEpSize, tt.args.torrentEpPath, tt.args.torrentEpSize)
		})
	}
}
