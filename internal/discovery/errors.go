package discovery

import (
	"errors"
	"fmt"
)

// ErrNotFound — sentinel «модуль/namespace не найден». Остальные ошибки Resolve
// (конфликт .yaml vs /config.yaml, ошибки ввода-вывода) его не оборачивают,
// поэтому вызывающий код не должен трактовать их как «не найдено».
var ErrNotFound = errors.New("discovery: not found")

// notFoundError сохраняет прежний текст сообщения и совпадает с ErrNotFound по errors.Is.
type notFoundError struct{ msg string }

func (e *notFoundError) Error() string        { return e.msg }
func (e *notFoundError) Is(target error) bool { return target == ErrNotFound }

// newModuleNotFound — ошибка «модуль не найден» с прежним текстом.
func newModuleNotFound(namespace string) error {
	return &notFoundError{msg: fmt.Sprintf("discovery: module %q not found", namespace)}
}
