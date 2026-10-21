package discovery

import (
	"errors"
	"fmt"
)

// ErrNotFound — sentinel «модуль/namespace не найден». Остальные ошибки Resolve
// (конфликт .yaml vs /config.yaml, ошибки ввода-вывода, в том числе stat) его не оборачивают,
// поэтому вызывающий код не должен трактовать их как «не найдено».
var ErrNotFound = errors.New("discovery: not found")

// ErrInvalidNamespace — namespace синтаксически неверен (например, пустой сегмент).
var ErrInvalidNamespace = errors.New("invalid namespace")

// notFoundError сохраняет прежний текст сообщения и совпадает с ErrNotFound по errors.Is.
type notFoundError struct{ msg string }

func (e *notFoundError) Error() string        { return e.msg }
func (e *notFoundError) Is(target error) bool { return target == ErrNotFound }

// newModuleNotFound — ошибка «модуль не найден» с прежним текстом.
func newModuleNotFound(namespace string) error {
	return &notFoundError{msg: fmt.Sprintf("discovery: module %q not found", namespace)}
}

// newNamespaceNotFound — ошибка «namespace не найден» в листингах с прежним
// текстом; совпадает с ErrNotFound, чтобы LayeredSource отличал отсутствие
// namespace в слое от конфликта и ошибок ввода-вывода.
func newNamespaceNotFound(namespace string) error {
	return &notFoundError{msg: fmt.Sprintf("discovery: namespace %q not found", namespace)}
}

// PartialError — листинг выполнен не полностью: часть источников или
// каталогов не прочитана. Результат, возвращённый вместе с ней, валиден,
// но неполон. Не совпадает с ErrNotFound.
type PartialError struct{ Errs []error }

func (e *PartialError) Error() string   { return errors.Join(e.Errs...).Error() }
func (e *PartialError) Unwrap() []error { return e.Errs }

// newPartialError возвращает nil для пустого списка ошибок и *PartialError иначе.
func newPartialError(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return &PartialError{Errs: errs}
}
