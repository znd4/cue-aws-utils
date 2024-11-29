package config

from_fixed: {
  for i, v in to_fixed
  {
	(v): i
  }
}
from_short: {
  for i, v in to_short
  {
	(v): i
  }
}
identity: {
  for k, v in to_short
  {
	(k): k
  }
}
