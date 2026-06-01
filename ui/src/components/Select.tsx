import React, { useState, useRef, useEffect } from 'react';
import { ChevronDown, Check } from 'lucide-react';
import { useIframePointerEvents } from '../hooks/useIframePointerEvents';

export interface SelectOption {
    value: string;
    label: string;
    icon?: React.ReactNode;
    description?: string;
}

interface SelectProps {
    value: string;
    onChange: (value: string) => void;
    options: SelectOption[];
    placeholder?: string;
    disabled?: boolean;
    className?: string;
}

export function Select({
    value,
    onChange,
    options,
    placeholder = 'Select an option',
    disabled = false,
    className = '',
}: SelectProps) {
    const [isOpen, setIsOpen] = useState(false);
    const dropdownRef = useRef<HTMLDivElement>(null);

    // When dropdown is open, disable iframe pointer-events so clicks penetrate
    // to the host document and trigger click-outside closing logic
    useIframePointerEvents(isOpen);

    const selectedOption = options.find(opt => opt.value === value);

    useEffect(() => {
        const handleClickOutside = (e: MouseEvent) => {
            if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
                setIsOpen(false);
            }
        };

        document.addEventListener('mousedown', handleClickOutside);
        return () => document.removeEventListener('mousedown', handleClickOutside);
    }, []);

    const handleSelect = (optionValue: string) => {
        onChange(optionValue);
        setIsOpen(false);
    };

    const handleKeyDown = (e: React.KeyboardEvent) => {
        if (disabled) return;

        if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            setIsOpen(!isOpen);
        } else if (e.key === 'Escape') {
            setIsOpen(false);
        } else if (e.key === 'ArrowDown' && isOpen) {
            e.preventDefault();
            const currentIndex = options.findIndex(opt => opt.value === value);
            const nextIndex = (currentIndex + 1) % options.length;
            onChange(options[nextIndex].value);
        } else if (e.key === 'ArrowUp' && isOpen) {
            e.preventDefault();
            const currentIndex = options.findIndex(opt => opt.value === value);
            const prevIndex = currentIndex <= 0 ? options.length - 1 : currentIndex - 1;
            onChange(options[prevIndex].value);
        }
    };

    return (
        <div ref={dropdownRef} className={`relative ${className}`}>
            <button
                type="button"
                onClick={() => !disabled && setIsOpen(!isOpen)}
                onKeyDown={handleKeyDown}
                disabled={disabled}
                className="w-full flex items-center justify-between gap-2 px-3 py-2.5 bg-deep border border-border-subtle rounded-lg text-sm text-text-primary hover:bg-hover focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/20 transition-all disabled:opacity-50 disabled:cursor-not-allowed"
                aria-haspopup="listbox"
                aria-expanded={isOpen}
            >
                <span className="flex items-center gap-2 truncate">
                    {selectedOption?.icon && <span className="flex-shrink-0">{selectedOption.icon}</span>}
                    <span className={selectedOption ? 'text-text-primary' : 'text-text-muted'}>
                        {selectedOption?.label || placeholder}
                    </span>
                </span>
                <ChevronDown
                    className={`w-4 h-4 text-text-muted transition-transform flex-shrink-0 ${isOpen ? 'rotate-180' : ''}`}
                />
            </button>

            {isOpen && !disabled && (
                <div
                    className="absolute z-50 w-full mt-1 bg-elevated border border-border-subtle rounded-lg shadow-lg overflow-y-auto max-h-60"
                    role="listbox"
                >
                    {options.map((option) => (
                        <button
                            key={option.value}
                            type="button"
                            onClick={() => handleSelect(option.value)}
                            className={`w-full flex items-center gap-2 px-3 py-2.5 text-sm text-left hover:bg-hover transition-colors ${option.value === value
                                    ? 'bg-accent/10 text-accent'
                                    : 'text-text-primary'
                                }`}
                            role="option"
                            aria-selected={option.value === value}
                        >
                            {option.icon && <span className="flex-shrink-0">{option.icon}</span>}
                            <span className="flex-1 truncate">{option.label}</span>
                            {option.value === value && (
                                <Check className="w-4 h-4 text-accent flex-shrink-0" />
                            )}
                        </button>
                    ))}
                </div>
            )}
        </div>
    );
}