package cmd

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mzz2017/gg/dialer"
)

var UnableToConnectErr = fmt.Errorf("unable to connect to the proxy node")

func GetDialerFromLink(nodeLink string, opt *dialer.GlobalOption, testNode bool, testURL string) (d *dialer.Dialer, err error) {
	u, err := url.Parse(nodeLink)
	if err != nil {
		return nil, err
	}
	d, err = dialer.NewFromLink(u.Scheme, u.String(), opt)
	if err != nil {
		return nil, err
	}
	if testNode {
		if ok, err := d.Test(context.Background(), testURL); !ok {
			return nil, fmt.Errorf("%w: %v", UnableToConnectErr, err)
		}
	}
	return d, nil
}
