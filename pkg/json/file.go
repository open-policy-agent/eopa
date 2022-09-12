package json

import (
	"io"
)

func newFile(content contentReader, offset int64) File {
	if offset < 0 {
		switch -offset {
		case typeNil:
			return nilV
		case typeFalse:
			return boolF
		case typeTrue:
			return boolT
		default:
			panic("json: corrupted file")
		}
	}

	t, err := content.ReadType(offset)
	checkError(err)

	switch t {
	case typeNil:
		return nilV

	case typeFalse:
		return boolF

	case typeTrue:
		return boolT

	case typeString:
		return newString(content, offset)

	case typeStringInt:
		return newStringInt(content, offset)

	case typeNumber:
		return newFloat(content, offset)

	case typeArray:
		return newArray(content, offset)

	case typeObjectFull, typeObjectThin, typeObjectPatch:
		return newObject(content, offset)

	case typeBinaryFull:
		return newBlob(content, offset)
	}

	corrupted(nil)

	// Never reached.
	return nil
}

func writeFile(content contentReader, offset int64, w io.Writer, written *int64) error {
	n, err := writeFileImpl(content, offset, w)
	*written += int64(n)
	return err
}

func writeFileImpl(content contentReader, offset int64, w io.Writer) (int64, error) {
	if offset < 0 {
		switch -offset {
		case typeNil:
			return nilV.WriteTo(w)
		case typeFalse:
			return boolF.WriteTo(w)
		case typeTrue:
			return boolT.WriteTo(w)
		default:
			panic("json: corrupted file")
		}
	}

	t, err := content.ReadType(offset)
	checkError(err)

	switch t {
	case typeNil:
		return nilV.WriteTo(w)

	case typeFalse:
		return boolF.WriteTo(w)

	case typeTrue:
		return boolT.WriteTo(w)

	case typeString:
		return newString(content, offset).WriteTo(w)

	case typeStringInt:
		return newStringInt(content, offset).WriteTo(w)

	case typeNumber:
		return newFloat(content, offset).WriteTo(w)

	case typeArray:
		return newArray(content, offset).WriteTo(w)

	case typeObjectFull, typeObjectThin, typeObjectPatch:
		return newObject(content, offset).WriteTo(w)

	case typeBinaryFull:
		return newBlob(content, offset).WriteTo(w)
	}

	corrupted(nil)

	// Never reached.
	return 0, nil
}

var (
	nilV  = NewNull()
	boolF = NewBool(false)
	boolT = NewBool(true)
)
