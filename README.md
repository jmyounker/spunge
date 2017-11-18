Spunge: Binary Spuoge
=====================

This program is modeled upon sponge, and is used for writing data from
a pipeline into the originating file.  Sponge requires Python, is limited
by memory, and does not do atomic file resplacement.  Spunge addresses
these limitations.

Motivation
==========

Standard Unix tooling has no way to replace the contents of a file in
a single step.  You'd like to be able to write:

```
> echo "foo" > /tmp/data.txt
> cat /tmp/data.txt | sed 's/foo/bar/' > /tmp/data.txt
> cat /tmp/data.txt
bar
```

But that's not what you get.  Instead:

```
> echo "foo" > /tmp/data.txt
> cat /tmp/data.txt | sed 's/foo/bar/' > /tmp/data.txt
> cat /tmp/data.txt
>

```

`Sponge` fixes this.  `Spunge` does it a little bit better.


Usage
-----
```
> echo "foo" > /tmp/data.txt
> cat /tmp/data.txt | sed 's/foo/bar/' | spunge /tmp/data.txt
> cat /tmp/data.txt
bar
>

```

This writes sed's stdout into a temporary file on the same directory
as `/tmp/data.txt`, and when `spunge`'s input closes, it moves the
temporary file to `/tmp/data.txt`.  The data is written as it is
received.  The original file is lost.


Just Like Sponge
----------------

The `--memory` option causes `spunge` to behave exactly like `sponge`.
It will accumulate all data in memory and then write it directly to the
original file. 

You can add the `--atomic` option to write to a different file and
then move it into place.


Preserving Old Files
--------------------

`Spunge` can preserve the original file using the `--suffix` option.

```
> echo "foo" > /tmp/data.txt
> ls /tmp
...
data.txt
...
> cat /tmp/data.txt | sed 's/foo/bar/' | spunge --suffix .old /tmp/data.txt
> ls /tmp
...
data.txt
data.txt.old
...
> cat /tmp/data.txt
bar
> cat /tmp/data.txt.old
foo
```

Normally `--prefix` copies the file to the new location, which means that
the process momentarily consumes double the disk space.  When used in
conjunction with the `--skinny-fast` option `spunge` instead moves the file
to the backup location.  This means that the file momentarily does not
exist.  If you're OK with this, then use `--skinny-fast`.

