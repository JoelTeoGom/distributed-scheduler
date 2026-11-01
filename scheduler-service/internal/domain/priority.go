package domain

// Priority is numeric so the jobs table can ORDER BY it: lower = more urgent.
type Priority uint8
