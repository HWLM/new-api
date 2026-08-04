/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useQuery } from '@tanstack/react-query'
import { Play } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Skeleton } from '@/components/ui/skeleton'

import { getTaskVideo } from '../api'

interface TaskVideoPreviewProps {
  taskId: string
}

function TaskVideoPreviewDialog(props: {
  taskId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const [videoUrl, setVideoUrl] = useState('')
  const videoQuery = useQuery({
    queryKey: ['task-video-preview', props.taskId],
    queryFn: () => getTaskVideo(props.taskId),
    retry: false,
    gcTime: 0,
  })

  useEffect(() => {
    if (!videoQuery.data) return

    const objectUrl = URL.createObjectURL(videoQuery.data)
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setVideoUrl(objectUrl)
    return () => URL.revokeObjectURL(objectUrl)
  }, [videoQuery.data])

  const isLoading = videoQuery.isPending || (videoQuery.data && !videoUrl)

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Video')}
      description={`${t('Task ID:')} ${props.taskId}`}
      contentClassName='sm:max-w-4xl'
      contentHeight='auto'
    >
      <div className='bg-muted/30 relative aspect-video min-h-48 overflow-hidden rounded-md border'>
        {isLoading && <Skeleton className='absolute inset-0 size-full' />}
        {videoQuery.isError && (
          <div
            role='alert'
            className='text-muted-foreground absolute inset-0 flex items-center justify-center text-sm'
          >
            {t('Failed to load')}
          </div>
        )}
        {videoUrl && (
          <video
            src={videoUrl}
            controls
            playsInline
            preload='metadata'
            aria-label={t('Video')}
            className='size-full object-contain'
          />
        )}
      </div>
    </Dialog>
  )
}

export function TaskVideoPreview(props: TaskVideoPreviewProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  return (
    <>
      <button
        type='button'
        className='group flex items-center gap-1 text-left text-xs'
        onClick={() => setOpen(true)}
      >
        <Play
          aria-hidden='true'
          className='text-muted-foreground size-3 fill-current'
        />
        <span className='text-foreground leading-snug group-hover:underline'>
          {t('Click to preview video')}
        </span>
      </button>
      {open && (
        <TaskVideoPreviewDialog
          taskId={props.taskId}
          open={open}
          onOpenChange={setOpen}
        />
      )}
    </>
  )
}
