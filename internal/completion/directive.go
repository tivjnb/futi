package completion

import "fmt"

// Directive — bit-field флагов, управляющих поведением shell после вывода подсказок.
// Значения соответствуют cobra ShellCompDirective для совместимости протокола.
type Directive int

const (
	// ShellCompDirectiveDefault — стандартное поведение: shell дополняет/выполняет,
	// после вывода подсказок возможно дополнение файлов.
	ShellCompDirectiveDefault Directive = 0

	// ShellCompDirectiveError — произошла ошибка; shell выводит сообщение об ошибке.
	ShellCompDirectiveError Directive = 1 << 0

	// ShellCompDirectiveNoSpace — не добавлять пробел после выбранной подсказки.
	ShellCompDirectiveNoSpace Directive = 1 << 1

	// ShellCompDirectiveNoFileComp — не предлагать дополнение файлов после подсказок.
	// Используется для futi по умолчанию: модули/команды/params — не файлы.
	ShellCompDirectiveNoFileComp Directive = 1 << 2

	// ShellCompDirectiveFilterFileExt — фильтровать файлы по расширению.
	ShellCompDirectiveFilterFileExt Directive = 1 << 3

	// ShellCompDirectiveFilterDirs — фильтровать только директории.
	ShellCompDirectiveFilterDirs Directive = 1 << 4
)

// String возвращает имя директивы (для stderr-сообщения об отладке).
func (d Directive) String() string {
	names := map[Directive]string{
		ShellCompDirectiveDefault:        "ShellCompDirectiveDefault",
		ShellCompDirectiveError:          "ShellCompDirectiveError",
		ShellCompDirectiveNoSpace:        "ShellCompDirectiveNoSpace",
		ShellCompDirectiveNoFileComp:     "ShellCompDirectiveNoFileComp",
		ShellCompDirectiveFilterFileExt:  "ShellCompDirectiveFilterFileExt",
		ShellCompDirectiveFilterDirs:     "ShellCompDirectiveFilterDirs",
	}

	// Комбинации флагов не имеют одиночного имени — выводим битовую сумму.
	if name, ok := names[d]; ok {
		return name
	}
	return fmt.Sprintf("ShellCompDirective(%d)", int(d))
}
