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

`Spunge` can preserve the original file using the `--backup` option.

```
> echo "foo" > /tmp/data.txt
> ls /tmp
...
data.txt
...
> cat /tmp/data.txt | sed 's/foo/bar/' | spunge --backup '{file}.old' /tmp/data.txt
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

There are three expansions in the backup filename:
  * `{file}` expands to the full target filename.
  * `{base}` expands to the target's name in the directory.  E.g. `/tmp/foo`
     has base of `foo`.
  * `{dir}` expands to the target's directory. E.g. `/tmp/foo` has dir of `/tmp`


Temp Directory
--------------

Normally the atomic sponge writes to a hidden file in the same directory containing
the target file, but you can specify another location using the `--tempdir` option.
The scratch file is written here and then afterwards copied to the destination
location.

The `--tmpdir` recognizes the `{dir}` option from the previous section.
