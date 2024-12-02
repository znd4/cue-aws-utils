package region_az_alt_code_maps

import (
	"github.com/znd4/cue-aws-utils:static"
)

to_fixed: static.to_fixed
to_short: static.to_short
from_fixed: {
	for i, v in to_fixed {
		(v): i
	}
}
from_short: {
	for i, v in to_short {
		(v): i
	}
}
identity: {
	for k, v in to_short {
		(k): k
	}
}
