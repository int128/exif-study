# Exif study

## Exif

http://www.cipa.jp/std/documents/e/DC-008-2012_E.pdf

http://www.exiv2.org/

### JPEG header

```
0000000 ff d8 ff e1 3c 14 45 78 69 66 00 00 49 49 2a 00
        ----- ----- ----- ----------------- ----- -----
        SOI   APP1  size  EXIF              LE    version

0000010 08 00 00 00
        -----------
        Offset
```

The APP1 segment has 0x3c14 (15,380) bytes. It is in little endian.

### 0th IFD

It has 9 tags, 108 bytes (12 bytes x 9 tags).
The tags begins at 0x16.

```
                          #1
0000010 08 00 00 00 09 00 0f 01 02 00 06 00 00 00 7a 00
                    ----- ----- ----- ----------- -----
                    Tags  Num   Type  Count       Offset
                                SLONG            
              #2                                  #3
0000020 00 00 10 01 02 00 14 00 00 00 80 00 00 00 12 01
        ----- ----- ----- ----------- -----------
              Num   Type  Count       Offset
                    SLONG
```


```
0000080 00 00 26 0a 00 00 43 61 6e 6f 6e 00 43 61 6e 6f
              -----------
              Offset to Next IFD
```

Next 2,598 bytes (0xa26) are payload.
Next IFD begins at 0x0aac (0x86 + 0x0a26).


## JFIF

https://www.w3.org/Graphics/JPEG/jfif3.pdf
