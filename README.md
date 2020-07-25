## About

The check plugin **check\_linux\_netdev** monitors
a Linux system's network device statistics via `/proc/net/dev`.

## Demonstration

1. `$ docker run -itp 8080:80 grandmaster/check_linux_netdev`
2. Open http://localhost:8080 and navigate to the (only) service

## Usage

The [plug-and-play Linux binaries]
take some optional CLI arguments and no environment variables:

```
$ ./check_linux_netdev [-d DURATION] [-e INTERFACE ...] [INTERFACE:METRIC:THRESHOLD=RANGE ...]
```

check\_linux\_netdev measures not only e.g. the bytes
every network device received so far, but also the average B/s
during DURATION being e.g. 10s or 2m (default: 1m).

**Yes, this plugin runs for one minute by default!**

-e specifies a network device to ignore.

INTERFACE specifies either one particular network device (e.g. "eth0")
or a pattern (e.g. "eth?\*") with the special characters
"?" (matches one character) and "*" (matches zero or more characters).

METRIC specifies a field of a network device in `/proc/net/dev`:

* "rx:bytes"
* "rx:packets"
* "rx:errs"
* "rx:drop"
* "rx:fifo"
* "rx:frame"
* "rx:compressed"
* "rx:multicast"
* "tx:bytes"
* "tx:packets"
* "tx:errs"
* "tx:drop"
* "tx:fifo"
* "tx:colls"
* "tx:carrier"
* "tx:compressed"

THRESHOLD specifies a warning/critical threshold:

* "total:w"
* "total:c"
* "persec:w"
* "persec:c"

"total" stands for the field value as-is,
"persec" for the average raise per second.
"w" stands for warning, "c" for critical.

RANGE is a threshold range as specified by the [Nagio$ check plugin API],
e.g. "23" or "@~:-42.0".

I.e. to let this plugin warn once an ethernet NIC
sends more than 1GB/s during 5 minutes:

```
$ ./check_linux_netdev -d 5m 'eth?*:tx:bytes:persec:w=1000000000' 'enp?*s?*:tx:bytes:persec:w=1000000000'
```

### Legal info

To print the legal info, execute the plugin in a terminal:

```
$ ./check_linux_netdev
```

In this case the program will always terminate with exit status 3 ("unknown")
without actually checking anything.

### Testing

If you want to actually execute a check inside a terminal,
you have to connect the standard output of the plugin to anything
other than a terminal – e.g. the standard input of another process:

```
$ ./check_linux_netdev |cat
```

In this case the exit code is likely to be the cat's one.
This can be worked around like this:

```
bash $ set -o pipefail
bash $ ./check_linux_netdev |cat
```

### Actual monitoring

Just integrate the plugin into the monitoring tool of your choice
like any other check plugin. (Consult that tool's manual on how to do that.)
It should work with any monitoring tool
supporting the [Nagio$ check plugin API].

The only limitation: check\_linux\_netdev must be run on the host to be checked
– either with an agent of your monitoring tool or by SSH.
Otherwise it will check the host your monitoring tool runs on.

Also take care of the execution timeout as **this plugin runs
for one minute by default** and the check interval as e.g. [Icinga 2]
seems to add the execution time to the check interval,
i.e. if you want to check via Icinga 2 every minute for one minute,
your check interval should be 1s.

#### Icinga 2

This repository ships the [check command definition]
as well as a [service template] and [host example] for Icinga 2.

The service definition will work in both correctly set up [Icinga 2 clusters]
and Icinga 2 instances not being part of any cluster
as long as the [hosts] are named after the [endpoints].

[plug-and-play Linux binaries]: https://github.com/Al2Klimov/check_linux_netdev/releases
[Nagio$ check plugin API]: https://nagios-plugins.org/doc/guidelines.html#AEN78
[check command definition]: ./icinga2/check_linux_netdev.conf
[service template]: ./icinga2/check_linux_netdev-service.conf
[host example]: ./icinga2/check_linux_netdev-host.conf
[Icinga 2]: https://www.icinga.com/docs/icinga2/latest/doc/01-about/
[Icinga 2 clusters]: https://www.icinga.com/docs/icinga2/latest/doc/06-distributed-monitoring/
[hosts]: https://www.icinga.com/docs/icinga2/latest/doc/09-object-types/#host
[endpoints]: https://www.icinga.com/docs/icinga2/latest/doc/09-object-types/#endpoint
